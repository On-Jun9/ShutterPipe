package pipeline

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/internal/config"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// TestPipelineShouldIncludeByDate_NoFilter는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineShouldIncludeByDate_NoFilter(t *testing.T) {
	// 날짜 필터가 없으면 모든 파일이 포함되어야 한다.
	p := &Pipeline{
		cfg: &config.Config{},
	}

	include := p.shouldIncludeByDate(types.FileEntry{}, types.MediaMetadata{})
	if !include {
		t.Fatal("expected include=true when no date filter")
	}
}

// TestPipelineShouldIncludeByDate_UsesCaptureTimeFirst는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineShouldIncludeByDate_UsesCaptureTimeFirst(t *testing.T) {
	// EXIF 캡처 시간이 있으면 파일 수정시간보다 우선해서 필터링해야 한다.
	capture := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	entry := types.FileEntry{
		ModTime: time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC), // 범위 밖
	}

	p := &Pipeline{
		cfg: &config.Config{
			DateFilterStart: "2025-01-01",
			DateFilterEnd:   "2025-01-31",
		},
	}

	include := p.shouldIncludeByDate(entry, types.MediaMetadata{CaptureTime: &capture})
	if !include {
		t.Fatal("expected include=true based on capture time")
	}
}

// TestPipelineShouldIncludeByDate_FallbackToModTime는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineShouldIncludeByDate_FallbackToModTime(t *testing.T) {
	// 캡처 시간이 없으면 수정시간으로 필터링해야 한다.
	entry := types.FileEntry{
		ModTime: time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC),
	}

	p := &Pipeline{
		cfg: &config.Config{
			DateFilterStart: "2025-02-01",
			DateFilterEnd:   "2025-02-10",
		},
	}

	include := p.shouldIncludeByDate(entry, types.MediaMetadata{})
	if !include {
		t.Fatal("expected include=true based on mod time fallback")
	}
}

// TestPipelineShouldIncludeByDate_InclusiveBounds는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineShouldIncludeByDate_InclusiveBounds(t *testing.T) {
	// 시작/종료 경계값 날짜는 포함되어야 한다.
	entry := types.FileEntry{
		ModTime: time.Date(2025, 3, 10, 23, 59, 59, 0, time.UTC),
	}

	p := &Pipeline{
		cfg: &config.Config{
			DateFilterStart: "2025-03-10",
			DateFilterEnd:   "2025-03-10",
		},
	}

	include := p.shouldIncludeByDate(entry, types.MediaMetadata{})
	if !include {
		t.Fatal("expected include=true on boundary date")
	}
}

// TestPipelineShouldIncludeByDate_ExcludedOutOfRange는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineShouldIncludeByDate_ExcludedOutOfRange(t *testing.T) {
	// 범위를 벗어난 날짜는 제외되어야 한다.
	entry := types.FileEntry{
		ModTime: time.Date(2025, 4, 20, 0, 0, 0, 0, time.UTC),
	}

	p := &Pipeline{
		cfg: &config.Config{
			DateFilterStart: "2025-04-01",
			DateFilterEnd:   "2025-04-10",
		},
	}

	include := p.shouldIncludeByDate(entry, types.MediaMetadata{})
	if include {
		t.Fatal("expected include=false for out-of-range date")
	}
}

// TestHistoryEntryID_UsesUnixNano는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHistoryEntryID_UsesUnixNano(t *testing.T) {
	// 히스토리 ID는 UnixNano 문자열이어야 한다.
	ts := time.Unix(0, 123456789)
	got := historyEntryID(ts)
	want := strconv.FormatInt(ts.UnixNano(), 10)

	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

// TestPipelineConfigToBackupConfig_MapsFields는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineConfigToBackupConfig_MapsFields(t *testing.T) {
	// 백업 히스토리 저장용 설정으로 주요 필드가 정확히 매핑되어야 한다.
	cfg := &config.Config{
		Source:            "/src",
		Dest:              "/dest",
		OrganizeStrategy:  types.OrganizeByEvent,
		EventName:         "wedding",
		ConflictPolicy:    types.ConflictPolicyRename,
		DedupMethod:       types.DedupMethodHash,
		DryRun:            true,
		HashVerify:        true,
		IgnoreState:       true,
		DateFilterStart:   "2025-01-01",
		DateFilterEnd:     "2025-01-31",
		Jobs:              7,
		IncludeExtensions: []string{"jpg", "mp4"},
		UnclassifiedDir:   "unc",
		QuarantineDir:     "quar",
	}

	p := &Pipeline{cfg: cfg}
	got := p.configToBackupConfig()
	want := types.BackupConfig{
		Source:            "/src",
		Dest:              "/dest",
		OrganizeStrategy:  types.OrganizeByEvent,
		EventName:         "wedding",
		ConflictPolicy:    types.ConflictPolicyRename,
		DedupMethod:       types.DedupMethodHash,
		DryRun:            true,
		HashVerify:        true,
		IgnoreState:       true,
		DateFilterStart:   "2025-01-01",
		DateFilterEnd:     "2025-01-31",
		Jobs:              7,
		IncludeExtensions: []string{"jpg", "mp4"},
		UnclassifiedDir:   "unc",
		QuarantineDir:     "quar",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected backup config:\nwant=%+v\ngot=%+v", want, got)
	}
}
