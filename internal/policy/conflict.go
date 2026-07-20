package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type ConflictResolver struct {
	policy        types.ConflictPolicy
	quarantineDir string
	mu            sync.Mutex
	reserved      map[string]reservation
}

type reservation struct {
	diskExisted bool
}

func NewConflictResolver(policy types.ConflictPolicy, quarantineDir string) *ConflictResolver {
	return &ConflictResolver{
		policy:        policy,
		quarantineDir: quarantineDir,
		reserved:      make(map[string]reservation),
	}
}

// ResetReservations scopes in-memory destination claims to one pipeline run.
func (c *ConflictResolver) ResetReservations() {
	c.mu.Lock()
	c.reserved = make(map[string]reservation)
	c.mu.Unlock()
}

type Resolution struct {
	Action          types.CopyAction
	DestPath        string
	Skip            bool
	ReplaceReserved bool
	// Err is set when the destination could not be inspected (e.g. EACCES/EIO or
	// an unreachable NAS mount). A backup tool must surface these as failures
	// instead of quietly skipping the file as if it already existed.
	Err error
}

func (c *ConflictResolver) Resolve(task *types.CopyTask) Resolution {
	c.mu.Lock()
	defer c.mu.Unlock()

	reserved, reservedConflict := c.reserved[task.DestPath]
	diskExisted, statErr := destinationExists(task.DestPath)
	if statErr != nil {
		return Resolution{Err: fmt.Errorf("목적지 상태를 확인하지 못했습니다 %s: %w", task.DestPath, statErr)}
	}
	if !reservedConflict && !diskExisted {
		c.reserve(task.DestPath, false)
		return Resolution{Action: types.CopyActionCopied, DestPath: task.DestPath}
	}

	switch c.policy {
	case types.ConflictPolicySkip:
		return Resolution{Action: types.CopyActionSkipped, Skip: true}

	case types.ConflictPolicyOverwrite:
		action := types.CopyActionCopied
		if diskExisted || reserved.diskExisted {
			action = types.CopyActionOverwritten
		}
		c.reserve(task.DestPath, diskExisted || reserved.diskExisted)
		return Resolution{Action: action, DestPath: task.DestPath, ReplaceReserved: reservedConflict}

	case types.ConflictPolicyRename:
		newPath, err := c.generateUniqueNameLocked(task.DestPath)
		if err != nil {
			return Resolution{Err: fmt.Errorf("이름 변경 후보를 확인하지 못했습니다 %s: %w", task.DestPath, err)}
		}
		c.reserve(newPath, false)
		return Resolution{Action: types.CopyActionRenamed, DestPath: newPath}

	case types.ConflictPolicyQuarantine:
		quarantinePath := filepath.Join(c.quarantineDir, task.Source.Name)
		quarantinePath, err := c.generateUniqueNameLocked(quarantinePath)
		if err != nil {
			return Resolution{Err: fmt.Errorf("격리 후보를 확인하지 못했습니다 %s: %w", quarantinePath, err)}
		}
		c.reserve(quarantinePath, false)
		return Resolution{Action: types.CopyActionQuarantined, DestPath: quarantinePath}

	default:
		return Resolution{Action: types.CopyActionSkipped, Skip: true}
	}
}

func (c *ConflictResolver) generateUniqueName(path string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generateUniqueNameLocked(path)
}

func (c *ConflictResolver) generateUniqueNameLocked(path string) (string, error) {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)

	for i := 1; i < 10000; i++ {
		newName := fmt.Sprintf("%s_%d%s", base, i, ext)
		newPath := filepath.Join(dir, newName)
		available, err := c.isAvailable(newPath)
		if err != nil {
			return "", err
		}
		if available {
			return newPath, nil
		}
	}

	// A backup tool must never report success without preserving the source.
	// Returning the original conflicting path here would let the caller count
	// the file as Skipped while nothing was copied.
	return "", fmt.Errorf("사용 가능한 이름 후보가 없습니다 (%s_1~_9999 모두 사용 중)", strings.TrimSuffix(filepath.Base(path), ext))
}

// isAvailable reports whether path can be claimed. A stat error other than a
// genuine ENOENT is returned so the caller can fail the file instead of quietly
// treating an unreadable candidate as unavailable and skipping it.
func (c *ConflictResolver) isAvailable(path string) (bool, error) {
	if _, ok := c.reserved[path]; ok {
		return false, nil
	}
	_, err := os.Stat(path)
	if err == nil {
		return false, nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, err
}

func (c *ConflictResolver) reserve(path string, diskExisted bool) {
	c.reserved[path] = reservation{diskExisted: diskExisted}
}

// destinationExists reports whether path exists, distinguishing a genuine
// "not found" from a stat failure. Only a real ENOENT means the destination is
// free; any other error must propagate so the caller can fail the file.
func destinationExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
