package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type Logger struct {
	mu      sync.Mutex
	console io.Writer
	file    *os.File
	logJSON bool
	logText bool
}

func New(logFilePath string, logJSON, logText bool) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(logFilePath), 0755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &Logger{
		console: os.Stdout,
		file:    file,
		logJSON: logJSON,
		logText: logText,
	}, nil
}

func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

type LogEntry struct {
	Timestamp time.Time        `json:"timestamp"`
	Level     string           `json:"level"`
	Message   string           `json:"message"`
	Source    string           `json:"source,omitempty"`
	Dest      string           `json:"dest,omitempty"`
	Action    types.CopyAction `json:"action,omitempty"`
	Error     string           `json:"error,omitempty"`
	Duration  time.Duration    `json:"duration,omitempty"`
}

func (l *Logger) LogTask(task types.CopyTask, duration time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   fmt.Sprintf("%s: %s -> %s", task.Action, task.Source.Name, task.DestPath),
		Source:    task.Source.Path,
		Dest:      task.DestPath,
		Action:    task.Action,
		Duration:  duration,
	}

	if task.Error != "" {
		entry.Level = "ERROR"
		entry.Error = task.Error
	}

	l.writeEntry(entry)
}

func (l *Logger) Info(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     "INFO",
		Message:   msg,
	}
	l.writeEntry(entry)
}

func (l *Logger) Error(msg string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     "ERROR",
		Message:   msg,
		Error:     err.Error(),
	}
	l.writeEntry(entry)
}

func (l *Logger) writeEntry(entry LogEntry) {
	if l.logJSON && l.file != nil {
		data, _ := json.Marshal(entry)
		l.file.Write(data)
		l.file.Write([]byte("\n"))
	}

	if l.logText && l.file != nil {
		line := fmt.Sprintf("[%s] %s %s\n",
			entry.Timestamp.Format("2006-01-02 15:04:05"),
			entry.Level,
			entry.Message,
		)
		if entry.Error != "" {
			line = fmt.Sprintf("[%s] %s %s - Error: %s\n",
				entry.Timestamp.Format("2006-01-02 15:04:05"),
				entry.Level,
				entry.Message,
				entry.Error,
			)
		}
		l.file.WriteString(line)
	}
}

func (l *Logger) Summary(summary types.RunSummary) {
	fmt.Fprintln(l.console, "\n=== ShutterPipe Summary ===")
	fmt.Fprintf(l.console, "Total files:    %d\n", summary.TotalFiles)
	fmt.Fprintf(l.console, "Copied:         %d\n", summary.Copied)
	fmt.Fprintf(l.console, "Skipped:        %d\n", summary.Skipped)
	fmt.Fprintf(l.console, "Renamed:        %d\n", summary.Renamed)
	fmt.Fprintf(l.console, "Overwritten:    %d\n", summary.Overwritten)
	fmt.Fprintf(l.console, "Quarantined:    %d\n", summary.Quarantined)
	fmt.Fprintf(l.console, "Failed:         %d\n", summary.Failed)
	fmt.Fprintf(l.console, "Unclassified:   %d\n", summary.Unclassified)
	fmt.Fprintf(l.console, "Duration:       %s\n", summary.Duration.Round(time.Second))
	if summary.BytesCopied > 0 {
		fmt.Fprintf(l.console, "Bytes copied:   %.2f MB\n", float64(summary.BytesCopied)/1024/1024)
		fmt.Fprintf(l.console, "Speed:          %.2f MB/s\n", summary.BytesPerSecond/1024/1024)
	}
	fmt.Fprintln(l.console, "===========================")
}

func (l *Logger) Progress(current, total int, filename string) {
	fmt.Fprintf(l.console, "\r[%d/%d] %s", current, total, filename)
}
