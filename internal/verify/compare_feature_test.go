package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

func TestIsBackupNameCandidate(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		destination string
		want        bool
	}{
		{name: "original", source: "Photo.JPG", destination: "photo.jpg", want: true},
		{name: "first rename", source: "Photo.JPG", destination: "photo_1.jpg", want: true},
		{name: "last rename", source: "Photo.JPG", destination: "PHOTO_9999.JPG", want: true},
		{name: "zero", source: "Photo.JPG", destination: "Photo_0.JPG"},
		{name: "zero padded", source: "Photo.JPG", destination: "Photo_01.JPG"},
		{name: "too large", source: "Photo.JPG", destination: "Photo_10000.JPG"},
		{name: "non numeric", source: "Photo.JPG", destination: "Photo_copy.JPG"},
		{name: "wrong extension", source: "Photo.JPG", destination: "Photo_1.PNG"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsBackupNameCandidate(test.source, test.destination); got != test.want {
				t.Fatalf("IsBackupNameCandidate(%q, %q) = %v, want %v", test.source, test.destination, got, test.want)
			}
		})
	}
}

func TestDestinationIndexQuickVerdictsAndPriority(t *testing.T) {
	root := t.TempDir()
	source := writeVerifyEntry(t, root, "source/Photo.JPG", "same")

	tests := []struct {
		name       string
		entries    []types.FileEntry
		incomplete bool
		want       types.VerifyVerdict
	}{
		{
			name: "normal renamed and case folded",
			entries: []types.FileEntry{
				{Name: "photo_2.jpg", Size: source.Size},
				{Name: "Photo_1.JPG", Size: source.Size + 1},
			},
			incomplete: true,
			want:       types.VerifyVerdictOK,
		},
		{
			name:    "mismatch",
			entries: []types.FileEntry{{Name: "photo.jpg", Size: source.Size + 1}},
			want:    types.VerifyVerdictMismatch,
		},
		{name: "missing", want: types.VerifyVerdictMissing},
		{
			name:       "unverifiable incomplete destination",
			entries:    []types.FileEntry{{Name: "other.jpg", Size: source.Size + 1}},
			incomplete: true,
			want:       types.VerifyVerdictUnverifiable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := NewDestinationIndex(test.entries, test.incomplete).CompareQuick(source)
			if result.Verdict != test.want {
				t.Fatalf("got %+v, want verdict %s", result, test.want)
			}
		})
	}
}

func TestDestinationIndexHashMatchesRenamedContentAndPrioritizesMatchOverReadError(t *testing.T) {
	root := t.TempDir()
	source := writeVerifyEntry(t, root, "source/original.jpg", "same-content")
	matching := writeVerifyEntry(t, root, "dest/completely-different.bin", "same-content")
	missing := types.FileEntry{
		Path: filepath.Join(root, "dest", "gone.bin"),
		Name: "gone.bin",
		Size: source.Size,
	}

	result := NewDestinationIndex([]types.FileEntry{missing, matching}, false).CompareHash(context.Background(), source)
	if result.Verdict != types.VerifyVerdictOK {
		t.Fatalf("matching content must win over another unreadable candidate: %+v", result)
	}
	if result.SourceHash == "" {
		t.Fatal("source hash was not retained")
	}
}

func TestDestinationIndexHashUnmatchedUnreadableCandidateIsUnverifiable(t *testing.T) {
	root := t.TempDir()
	source := writeVerifyEntry(t, root, "source/original.jpg", "same-content")
	missing := types.FileEntry{
		Path: filepath.Join(root, "dest", "gone.bin"),
		Name: "gone.bin",
		Size: source.Size,
	}

	result := NewDestinationIndex([]types.FileEntry{missing}, false).CompareHash(context.Background(), source)
	if result.Verdict != types.VerifyVerdictUnverifiable {
		t.Fatalf("got %+v", result)
	}
}

func writeVerifyEntry(t *testing.T, root, relativePath, content string) types.FileEntry {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return types.FileEntry{
		Path: path, Name: filepath.Base(path), Size: info.Size(), ModTime: info.ModTime(),
		Extension: "jpg",
	}
}
