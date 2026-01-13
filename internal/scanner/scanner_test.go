package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanner_Scan(t *testing.T) {
	tmpDir := t.TempDir()

	testFiles := []struct {
		name    string
		content string
	}{
		{"photo1.jpg", "fake jpg"},
		{"photo2.JPEG", "fake jpeg"},
		{"video1.mp4", "fake mp4"},
		{"document.pdf", "should be ignored"},
		{"subdir/photo3.heic", "nested photo"},
	}

	for _, tf := range testFiles {
		path := filepath.Join(tmpDir, tf.name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(tf.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	s := New([]string{"jpg", "jpeg", "heic", "mp4"})
	entries, err := s.Scan(tmpDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(entries) != 4 {
		t.Errorf("expected 4 files, got %d", len(entries))
	}

	videoCount := 0
	for _, e := range entries {
		if e.IsVideo {
			videoCount++
		}
	}
	if videoCount != 1 {
		t.Errorf("expected 1 video, got %d", videoCount)
	}
}
