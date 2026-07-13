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
}

func (c *ConflictResolver) Resolve(task *types.CopyTask) Resolution {
	c.mu.Lock()
	defer c.mu.Unlock()

	reserved, reservedConflict := c.reserved[task.DestPath]
	_, statErr := os.Stat(task.DestPath)
	diskExisted := !os.IsNotExist(statErr)
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
		newPath := c.generateUniqueNameLocked(task.DestPath)
		if !c.isAvailable(newPath) {
			return Resolution{Action: types.CopyActionSkipped, Skip: true}
		}
		c.reserve(newPath, false)
		return Resolution{Action: types.CopyActionRenamed, DestPath: newPath}

	case types.ConflictPolicyQuarantine:
		quarantinePath := filepath.Join(c.quarantineDir, task.Source.Name)
		quarantinePath = c.generateUniqueNameLocked(quarantinePath)
		if !c.isAvailable(quarantinePath) {
			return Resolution{Action: types.CopyActionSkipped, Skip: true}
		}
		c.reserve(quarantinePath, false)
		return Resolution{Action: types.CopyActionQuarantined, DestPath: quarantinePath}

	default:
		return Resolution{Action: types.CopyActionSkipped, Skip: true}
	}
}

func (c *ConflictResolver) generateUniqueName(path string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generateUniqueNameLocked(path)
}

func (c *ConflictResolver) generateUniqueNameLocked(path string) string {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)

	for i := 1; i < 10000; i++ {
		newName := fmt.Sprintf("%s_%d%s", base, i, ext)
		newPath := filepath.Join(dir, newName)
		if c.isAvailable(newPath) {
			return newPath
		}
	}

	return path
}

func (c *ConflictResolver) isAvailable(path string) bool {
	if _, ok := c.reserved[path]; ok {
		return false
	}
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

func (c *ConflictResolver) reserve(path string, diskExisted bool) {
	c.reserved[path] = reservation{diskExisted: diskExisted}
}
