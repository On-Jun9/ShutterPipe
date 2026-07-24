package verify

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// HashStableFileWithContext hashes one stable file snapshot and rejects a file
// that changes identity, size, or modification time while it is being read.
func HashStableFileWithContext(ctx context.Context, path string) (string, os.FileInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	file, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()

	before, err := file.Stat()
	if err != nil {
		return "", nil, err
	}
	if !before.Mode().IsRegular() {
		return "", nil, fmt.Errorf("not a regular file: %s", path)
	}

	hasher := sha256.New()
	buffer := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		read, readErr := file.Read(buffer)
		if read > 0 {
			if _, err := hasher.Write(buffer[:read]); err != nil {
				return "", nil, err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", nil, readErr
		}
	}

	after, err := file.Stat()
	if err != nil {
		return "", nil, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", nil, fmt.Errorf("file changed while hashing: %s", path)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), after, nil
}
