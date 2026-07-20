package verify

import (
	"bytes"
	"context"
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
	return v.VerifyWithContext(context.Background(), srcPath, destPath, expectedSize)
}

func (v *Verifier) VerifyWithContext(ctx context.Context, srcPath, destPath string, expectedSize int64) error {
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

	srcHash, err := hashFileWithContext(ctx, srcPath)
	if err != nil {
		return fmt.Errorf("failed to hash source: %w", err)
	}

	destHash, err := hashFileWithContext(ctx, destPath)
	if err != nil {
		return fmt.Errorf("failed to hash destination: %w", err)
	}

	if srcHash != destHash {
		return fmt.Errorf("hash mismatch: src=%s, dest=%s", srcHash, destHash)
	}

	return nil
}

func (v *Verifier) VerifyStagedWithContext(ctx context.Context, stagedPath string, expectedSize int64, expectedHash []byte) error {
	file, err := os.Open(stagedPath)
	if err != nil {
		return fmt.Errorf("staged file not found: %w", err)
	}
	defer file.Close()
	return v.VerifyStagedFileWithContext(ctx, file, expectedSize, expectedHash)
}

func (v *Verifier) VerifyStagedFileWithContext(ctx context.Context, file *os.File, expectedSize int64, expectedHash []byte) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat staged file: %w", err)
	}
	if info.Size() != expectedSize {
		return fmt.Errorf("size mismatch: expected %d, got %d", expectedSize, info.Size())
	}
	if !v.hashVerify {
		return nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to rewind staged file: %w", err)
	}
	actualHash, err := hashReaderWithContext(ctx, file)
	if err != nil {
		return fmt.Errorf("failed to hash staged file: %w", err)
	}
	if !bytes.Equal(expectedHash, actualHash) {
		return fmt.Errorf("hash mismatch: expected=%x, staged=%x", expectedHash, actualHash)
	}
	return nil
}

func (v *Verifier) VerifySourceHashWithContext(ctx context.Context, sourcePath string, expectedHash []byte) error {
	f, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to reopen source: %w", err)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source before hash: %w", err)
	}
	h := sha256.New()
	buf := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			if _, err := h.Write(buf[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	after, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source after hash: %w", err)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("source changed while rehashing: %s", sourcePath)
	}
	if !bytes.Equal(expectedHash, h.Sum(nil)) {
		return fmt.Errorf("source hash changed during copy: %s", sourcePath)
	}
	return nil
}

func hashFile(path string) (string, error) {
	return hashFileWithContext(context.Background(), path)
}

func hashFileWithContext(ctx context.Context, path string) (string, error) {
	hash, err := hashFileBytesWithContext(ctx, path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash), nil
}

func hashFileBytesWithContext(ctx context.Context, path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return hashReaderWithContext(ctx, f)
}

func hashReaderWithContext(ctx context.Context, r io.Reader) ([]byte, error) {
	h := sha256.New()
	buf := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, readErr := r.Read(buf)
		if n > 0 {
			if _, err := h.Write(buf[:n]); err != nil {
				return nil, err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return h.Sum(nil), nil
}
