package log

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// TestLogger_WritesTextEntriesToFile는 테스트 코드 동작을 검증하거나 보조합니다.
func TestLogger_WritesTextEntriesToFile(t *testing.T) {
	// 텍스트 로깅 모드에서 Info/Error/LogTask가 파일에 기록되어야 한다.
	logPath := filepath.Join(t.TempDir(), "logs", "app.log")
	logger, err := New(logPath, false, true)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	logger.Info("hello")
	logger.Error("failed op", errors.New("boom"))
	logger.LogTask(types.CopyTask{
		Source:   types.FileEntry{Name: "a.jpg", Path: "/src/a.jpg"},
		DestPath: "/dest/a.jpg",
		Action:   types.CopyActionCopied,
	}, 10*time.Millisecond)

	if err := logger.Close(); err != nil {
		t.Fatalf("failed to close logger: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	text := string(data)

	if !strings.Contains(text, "INFO hello") {
		t.Fatalf("missing info log line: %s", text)
	}
	if !strings.Contains(text, "ERROR failed op - Error: boom") {
		t.Fatalf("missing error log line: %s", text)
	}
	if !strings.Contains(text, "copied: a.jpg -> /dest/a.jpg") {
		t.Fatalf("missing task log line: %s", text)
	}
}

// TestLogger_JSONModeWritesJSONLine는 테스트 코드 동작을 검증하거나 보조합니다.
func TestLogger_JSONModeWritesJSONLine(t *testing.T) {
	// JSON 로깅 모드에서는 한 줄 JSON 레코드가 출력되어야 한다.
	logPath := filepath.Join(t.TempDir(), "logs", "app.jsonl")
	logger, err := New(logPath, true, false)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	logger.Info("json-message")
	if err := logger.Close(); err != nil {
		t.Fatalf("failed to close logger: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read json log file: %v", err)
	}
	if !strings.Contains(string(data), `"message":"json-message"`) {
		t.Fatalf("unexpected json log content: %s", string(data))
	}
}

// TestLogger_SummaryAndProgress_WriteToConsole는 테스트 코드 동작을 검증하거나 보조합니다.
func TestLogger_SummaryAndProgress_WriteToConsole(t *testing.T) {
	// Summary/Progress 출력은 console writer로 전달되어야 한다.
	var buf bytes.Buffer
	logger := &Logger{console: &buf}

	logger.Summary(types.RunSummary{
		TotalFiles:     2,
		Copied:         1,
		Skipped:        1,
		Duration:       2 * time.Second,
		BytesCopied:    1024,
		BytesPerSecond: 512,
	})
	logger.Progress(1, 2, "a.jpg")

	out := buf.String()
	if !strings.Contains(out, "ShutterPipe Summary") {
		t.Fatalf("missing summary header: %s", out)
	}
	if !strings.Contains(out, "[1/2] a.jpg") {
		t.Fatalf("missing progress output: %s", out)
	}
}

// TestLogger_CloseWithNilFile는 테스트 코드 동작을 검증하거나 보조합니다.
func TestLogger_CloseWithNilFile(t *testing.T) {
	// 파일 핸들이 없는 로거는 Close 시 에러 없이 종료되어야 한다.
	logger := &Logger{}
	if err := logger.Close(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}
