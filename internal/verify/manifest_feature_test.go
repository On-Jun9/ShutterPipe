package verify

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

func TestParseManifestSupportsGNUVariantsAndEscapedPaths(t *testing.T) {
	firstHash := fmt.Sprintf("%x", sha256.Sum256([]byte("first")))
	secondHash := fmt.Sprintf("%x", sha256.Sum256([]byte("second")))
	content := strings.Join([]string{
		firstHash + "  folder/photo.jpg",
		secondHash + " *folder/video.mp4",
		`\` + firstHash + `  folder\\line\nbreak.jpg`,
	}, "\n")

	manifest, err := ParseManifest(strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ParseErrors != 0 || len(manifest.Entries) != 3 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if manifest.Entries[2].Path != "folder\\line\nbreak.jpg" {
		t.Fatalf("escaped path = %q", manifest.Entries[2].Path)
	}
}

func TestParseManifestCountsEveryMalformedOrBlankLine(t *testing.T) {
	validHash := fmt.Sprintf("%x", sha256.Sum256([]byte("ok")))
	content := strings.Join([]string{
		validHash + "  ok.jpg",
		"",
		"not-a-hash  bad.jpg",
		`\` + validHash + `  bad\tescape.jpg`,
	}, "\n")

	manifest, err := ParseManifest(strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 1 || manifest.ParseErrors != 3 {
		t.Fatalf("entries=%d parseErrors=%d", len(manifest.Entries), manifest.ParseErrors)
	}
}

func TestManifestParseErrorOnlyAllowsPositiveMatches(t *testing.T) {
	root := t.TempDir()
	matching := writeVerifyEntry(t, root, "source/matching.jpg", "matching")
	unmatched := writeVerifyEntry(t, root, "source/unmatched.jpg", "unmatched")
	matchingHash, _, err := HashStableFileWithContext(context.Background(), matching.Path)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest(strings.NewReader(matchingHash + "  renamed.bin\ninvalid\n"))
	if err != nil {
		t.Fatal(err)
	}

	if got := manifest.Compare(context.Background(), matching); got.Verdict != types.VerifyVerdictOK {
		t.Fatalf("positive match was not confirmed: %+v", got)
	}
	if got := manifest.Compare(context.Background(), unmatched); got.Verdict != types.VerifyVerdictUnverifiable {
		t.Fatalf("incomplete manifest confirmed a negative result: %+v", got)
	}
}

func TestManifestUsesBasenameOnlyForMismatch(t *testing.T) {
	root := t.TempDir()
	source := writeVerifyEntry(t, root, "source/photo.jpg", "source")
	otherHash := fmt.Sprintf("%x", sha256.Sum256([]byte("other")))
	manifest, err := ParseManifest(strings.NewReader(otherHash + "  /untrusted/path/PHOTO_1.JPG"))
	if err != nil {
		t.Fatal(err)
	}

	result := manifest.Compare(context.Background(), source)
	if result.Verdict != types.VerifyVerdictMismatch {
		t.Fatalf("got %+v", result)
	}
}
