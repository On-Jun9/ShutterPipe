package policy

import (
	"context"
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
	return d.IsDuplicateWithContext(context.Background(), src, destPath)
}

func (d *DedupChecker) IsDuplicateWithContext(ctx context.Context, src types.FileEntry, destPath string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

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

	srcHash, err := hashFileWithContext(ctx, src.Path)
	if err != nil {
		return false, err
	}

	destHash, err := hashFileWithContext(ctx, destPath)
	if err != nil {
		return false, err
	}

	return srcHash == destHash, nil
}

func hashFileWithContext(ctx context.Context, path string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	buf := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		n, readErr := f.Read(buf)
		if n > 0 {
			if _, err := h.Write(buf[:n]); err != nil {
				return "", err
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
