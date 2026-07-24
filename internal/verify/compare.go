package verify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type CompareResult struct {
	Verdict    types.VerifyVerdict
	Reason     string
	SourceHash string
	Ambiguous  bool
}

type cachedHash struct {
	value string
	err   error
}

// DestinationIndex is a read-only destination snapshot shared by every source
// comparison in one verification run.
type DestinationIndex struct {
	entries    []types.FileEntry
	bySize     map[int64][]types.FileEntry
	incomplete bool
	hashes     map[string]cachedHash
}

func NewDestinationIndex(entries []types.FileEntry, incomplete bool) *DestinationIndex {
	index := &DestinationIndex{
		entries:    append([]types.FileEntry(nil), entries...),
		bySize:     make(map[int64][]types.FileEntry),
		incomplete: incomplete,
		hashes:     make(map[string]cachedHash),
	}
	for _, entry := range entries {
		index.bySize[entry.Size] = append(index.bySize[entry.Size], entry)
	}
	return index
}

func (i *DestinationIndex) CompareQuick(source types.FileEntry) CompareResult {
	if err := validateSourceSnapshot(source); err != nil {
		return CompareResult{
			Verdict: types.VerifyVerdictUnverifiable,
			Reason:  "원본 파일을 확인하지 못했습니다: " + err.Error(),
		}
	}

	candidates := i.nameCandidates(source.Name)
	for _, candidate := range candidates {
		if candidate.Size == source.Size {
			return CompareResult{
				Verdict:   types.VerifyVerdictOK,
				Reason:    "이름과 크기가 일치하는 파일이 있습니다.",
				Ambiguous: len(candidates) > 1,
			}
		}
	}
	if i.incomplete {
		return CompareResult{
			Verdict: types.VerifyVerdictUnverifiable,
			Reason:  "읽지 못한 도착 경로가 있어 일치 파일의 부재를 확정할 수 없습니다.",
		}
	}
	if len(candidates) > 0 {
		return CompareResult{
			Verdict:   types.VerifyVerdictMismatch,
			Reason:    "같은 이름의 후보는 있으나 크기가 다릅니다.",
			Ambiguous: len(candidates) > 1,
		}
	}
	return CompareResult{
		Verdict: types.VerifyVerdictMissing,
		Reason:  "같은 이름과 크기의 파일이 없습니다.",
	}
}

func (i *DestinationIndex) CompareHash(ctx context.Context, source types.FileEntry) CompareResult {
	sourceHash, sourceInfo, err := HashStableFileWithContext(ctx, source.Path)
	if err != nil {
		return CompareResult{
			Verdict: types.VerifyVerdictUnverifiable,
			Reason:  "원본 파일을 읽지 못했습니다: " + err.Error(),
		}
	}
	if sourceInfo.Size() != source.Size || !source.ModTime.IsZero() && !sourceInfo.ModTime().Equal(source.ModTime) {
		return CompareResult{
			Verdict: types.VerifyVerdictUnverifiable,
			Reason:  "검증 중 원본 파일이 변경되었습니다.",
		}
	}

	unreadable := false
	for _, candidate := range i.bySize[source.Size] {
		hash := i.hashDestination(ctx, candidate)
		if hash.err != nil {
			unreadable = true
			continue
		}
		if hash.value == sourceHash {
			return CompareResult{
				Verdict:    types.VerifyVerdictOK,
				Reason:     "같은 내용의 파일이 도착 폴더에 있습니다.",
				SourceHash: sourceHash,
			}
		}
	}

	if i.incomplete || unreadable {
		return CompareResult{
			Verdict:    types.VerifyVerdictUnverifiable,
			Reason:     "읽지 못한 도착 후보가 있어 같은 내용의 파일이 없는지 확정할 수 없습니다.",
			SourceHash: sourceHash,
		}
	}
	if len(i.nameCandidates(source.Name)) > 0 {
		return CompareResult{
			Verdict:    types.VerifyVerdictMismatch,
			Reason:     "같은 이름의 후보는 있으나 내용이 다릅니다.",
			SourceHash: sourceHash,
		}
	}
	return CompareResult{
		Verdict:    types.VerifyVerdictMissing,
		Reason:     "같은 내용의 파일이 도착 폴더에 없습니다.",
		SourceHash: sourceHash,
	}
}

func (i *DestinationIndex) hashDestination(ctx context.Context, entry types.FileEntry) cachedHash {
	if cached, ok := i.hashes[entry.Path]; ok {
		return cached
	}
	value, info, err := HashStableFileWithContext(ctx, entry.Path)
	if err == nil && (info.Size() != entry.Size || !entry.ModTime.IsZero() && !info.ModTime().Equal(entry.ModTime)) {
		err = fmt.Errorf("destination changed since scan: %s", entry.Path)
	}
	cached := cachedHash{value: value, err: err}
	i.hashes[entry.Path] = cached
	return cached
}

func (i *DestinationIndex) nameCandidates(sourceName string) []types.FileEntry {
	var candidates []types.FileEntry
	for _, entry := range i.entries {
		if IsBackupNameCandidate(sourceName, entry.Name) {
			candidates = append(candidates, entry)
		}
	}
	return candidates
}

func validateSourceSnapshot(source types.FileEntry) error {
	file, err := os.Open(source.Path)
	if err != nil {
		return err
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil {
		return statErr
	}
	if closeErr != nil {
		return closeErr
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if info.Size() != source.Size || !source.ModTime.IsZero() && !info.ModTime().Equal(source.ModTime) {
		return fmt.Errorf("source changed since scan")
	}
	return nil
}

// IsBackupNameCandidate recognizes the original filename and the _N names
// produced by the rename conflict policy.
func IsBackupNameCandidate(sourceName, destinationName string) bool {
	if strings.EqualFold(sourceName, destinationName) {
		return true
	}

	sourceExt := filepath.Ext(sourceName)
	destinationExt := filepath.Ext(destinationName)
	if !strings.EqualFold(sourceExt, destinationExt) {
		return false
	}
	sourceBase := strings.TrimSuffix(sourceName, sourceExt)
	destinationBase := strings.TrimSuffix(destinationName, destinationExt)
	if len(destinationBase) <= len(sourceBase)+1 ||
		!strings.EqualFold(destinationBase[:len(sourceBase)], sourceBase) ||
		destinationBase[len(sourceBase)] != '_' {
		return false
	}
	suffix := destinationBase[len(sourceBase)+1:]
	number, err := strconv.Atoi(suffix)
	return err == nil && number >= 1 && number <= 9999 && strconv.Itoa(number) == suffix
}
