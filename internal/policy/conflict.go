package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type ConflictResolver struct {
	policy        types.ConflictPolicy
	quarantineDir string
}

func NewConflictResolver(policy types.ConflictPolicy, quarantineDir string) *ConflictResolver {
	return &ConflictResolver{
		policy:        policy,
		quarantineDir: quarantineDir,
	}
}

type Resolution struct {
	Action   types.CopyAction
	DestPath string
	Skip     bool
}

func (c *ConflictResolver) Resolve(task *types.CopyTask) Resolution {
	if _, err := os.Stat(task.DestPath); os.IsNotExist(err) {
		return Resolution{Action: types.CopyActionCopied, DestPath: task.DestPath}
	}

	switch c.policy {
	case types.ConflictPolicySkip:
		return Resolution{Action: types.CopyActionSkipped, Skip: true}

	case types.ConflictPolicyOverwrite:
		return Resolution{Action: types.CopyActionOverwritten, DestPath: task.DestPath}

	case types.ConflictPolicyRename:
		newPath := c.generateUniqueName(task.DestPath)
		return Resolution{Action: types.CopyActionRenamed, DestPath: newPath}

	case types.ConflictPolicyQuarantine:
		quarantinePath := filepath.Join(c.quarantineDir, task.Source.Name)
		quarantinePath = c.generateUniqueName(quarantinePath)
		return Resolution{Action: types.CopyActionQuarantined, DestPath: quarantinePath}

	default:
		return Resolution{Action: types.CopyActionSkipped, Skip: true}
	}
}

func (c *ConflictResolver) generateUniqueName(path string) string {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)

	for i := 1; i < 10000; i++ {
		newName := fmt.Sprintf("%s_%d%s", base, i, ext)
		newPath := filepath.Join(dir, newName)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath
		}
	}

	return path
}
