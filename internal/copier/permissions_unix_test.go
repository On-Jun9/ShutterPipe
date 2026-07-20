//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package copier

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

func TestCopierRespectsRestrictiveUmask(t *testing.T) {
	oldUmask := syscall.Umask(0077)
	defer syscall.Umask(oldUmask)

	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "source.jpg")
	destRoot := filepath.Join(tmpDir, "destination")
	dest := filepath.Join(destRoot, "photo.jpg")
	if err := os.WriteFile(source, []byte("private photo"), 0600); err != nil {
		t.Fatal(err)
	}

	result := New(1, false, false).copyOne(context.Background(), types.CopyTask{
		Source:          types.FileEntry{Path: source, Name: "source.jpg", Size: 13},
		DestPath:        dest,
		DestinationRoot: destRoot,
		ConflictPolicy:  types.ConflictPolicySkip,
		Action:          types.CopyActionCopied,
	})
	if result.Error != nil {
		t.Fatalf("copy failed: %v", result.Error)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("backup file ignored umask 0077: mode=%#o", got)
	}
}
