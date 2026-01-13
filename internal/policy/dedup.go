package policy

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type DedupChecker struct {
	method types.DedupMethod
}

func NewDedupChecker(method types.DedupMethod) *DedupChecker {
	return &DedupChecker{method: method}
}

func (d *DedupChecker) IsDuplicate(src types.FileEntry, destPath string) (bool, error) {
	destInfo, err := os.Stat(destPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if d.method == types.DedupMethodNameSize {
		return src.Size == destInfo.Size(), nil
	}

	srcHash, err := hashFile(src.Path)
	if err != nil {
		return false, err
	}

	destHash, err := hashFile(destPath)
	if err != nil {
		return false, err
	}

	return srcHash == destHash, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
