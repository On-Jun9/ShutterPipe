package verify

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

type Verifier struct {
	hashVerify bool
}

func New(hashVerify bool) *Verifier {
	return &Verifier{hashVerify: hashVerify}
}

func (v *Verifier) Verify(srcPath, destPath string, expectedSize int64) error {
	destInfo, err := os.Stat(destPath)
	if err != nil {
		return fmt.Errorf("destination file not found: %w", err)
	}

	if destInfo.Size() != expectedSize {
		return fmt.Errorf("size mismatch: expected %d, got %d", expectedSize, destInfo.Size())
	}

	if !v.hashVerify {
		return nil
	}

	srcHash, err := hashFile(srcPath)
	if err != nil {
		return fmt.Errorf("failed to hash source: %w", err)
	}

	destHash, err := hashFile(destPath)
	if err != nil {
		return fmt.Errorf("failed to hash destination: %w", err)
	}

	if srcHash != destHash {
		return fmt.Errorf("hash mismatch: src=%s, dest=%s", srcHash, destHash)
	}

	return nil
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
