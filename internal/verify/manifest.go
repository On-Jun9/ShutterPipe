package verify

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type ManifestEntry struct {
	Hash string
	Path string
	Name string
}

type Manifest struct {
	Entries     []ManifestEntry
	ParseErrors int
	byHash      map[string]struct{}
}

func ParseManifest(reader io.Reader) (*Manifest, error) {
	manifest := &Manifest{byHash: make(map[string]struct{})}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 65*1024*1024)
	for scanner.Scan() {
		// CRLF(Windows) 매니페스트의 끝 \r을 제거한다. 없으면 basename 비교에
		// \r이 섞여 mismatch가 missing으로 잘못 강등될 수 있다.
		entry, err := parseManifestLine(strings.TrimRight(scanner.Text(), "\r"))
		if err != nil {
			manifest.ParseErrors++
			continue
		}
		manifest.Entries = append(manifest.Entries, entry)
		manifest.byHash[entry.Hash] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (m *Manifest) Compare(ctx context.Context, source types.FileEntry) CompareResult {
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
	if _, ok := m.byHash[sourceHash]; ok {
		return CompareResult{
			Verdict:    types.VerifyVerdictOK,
			Reason:     "목록에 같은 내용의 파일이 있습니다.",
			SourceHash: sourceHash,
		}
	}
	if m.ParseErrors > 0 {
		return CompareResult{
			Verdict:    types.VerifyVerdictUnverifiable,
			Reason:     "해시 목록에 형식 오류가 있어 파일의 부재를 확정할 수 없습니다.",
			SourceHash: sourceHash,
		}
	}
	for _, entry := range m.Entries {
		if IsBackupNameCandidate(source.Name, entry.Name) {
			return CompareResult{
				Verdict:    types.VerifyVerdictMismatch,
				Reason:     "같은 이름은 목록에 있으나 내용이 다릅니다.",
				SourceHash: sourceHash,
			}
		}
	}
	return CompareResult{
		Verdict:    types.VerifyVerdictMissing,
		Reason:     "목록에 같은 내용의 파일이 없습니다.",
		SourceHash: sourceHash,
	}
}

func parseManifestLine(line string) (ManifestEntry, error) {
	if line == "" {
		return ManifestEntry{}, fmt.Errorf("empty line")
	}
	escaped := strings.HasPrefix(line, `\`)
	if escaped {
		line = line[1:]
	}
	if len(line) < 67 || line[64] != ' ' || line[65] != ' ' && line[65] != '*' {
		return ManifestEntry{}, fmt.Errorf("invalid sha256sum line")
	}
	hash := strings.ToLower(line[:64])
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != 32 {
		return ManifestEntry{}, fmt.Errorf("invalid SHA-256 digest")
	}
	path := line[66:]
	if path == "" {
		return ManifestEntry{}, fmt.Errorf("missing path")
	}
	if escaped {
		path, err = unescapeManifestPath(path)
		if err != nil {
			return ManifestEntry{}, err
		}
	}
	return ManifestEntry{Hash: hash, Path: path, Name: filepath.Base(path)}, nil
}

func unescapeManifestPath(path string) (string, error) {
	var result strings.Builder
	result.Grow(len(path))
	for index := 0; index < len(path); index++ {
		if path[index] != '\\' {
			result.WriteByte(path[index])
			continue
		}
		index++
		if index >= len(path) {
			return "", fmt.Errorf("trailing manifest escape")
		}
		switch path[index] {
		case '\\':
			result.WriteByte('\\')
		case 'n':
			result.WriteByte('\n')
		case 'r':
			result.WriteByte('\r')
		default:
			return "", fmt.Errorf("unsupported manifest escape")
		}
	}
	return result.String(), nil
}
