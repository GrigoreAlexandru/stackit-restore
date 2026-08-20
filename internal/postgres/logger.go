package postgres

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ExecutionLogger manages concurrent logging to an in-memory transcript buffer
// and an optional custom output destination.
type ExecutionLogger struct {
	mu     sync.RWMutex
	buf    bytes.Buffer
	writer io.Writer
}

// NewExecutionLogger returns an initialized ExecutionLogger instance.
func NewExecutionLogger(w io.Writer) *ExecutionLogger {
	return &ExecutionLogger{
		writer: w,
	}
}

// Append safely writes data to the execution buffer. Safe to call on nil.
func (l *ExecutionLogger) Append(data []byte) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.Write(data)
}

// GetLog returns the entire captured execution buffer. Safe to call on nil.
func (l *ExecutionLogger) GetLog() string {
	if l == nil {
		return ""
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.buf.String()
}

// Reset clears the execution buffer. Safe to call on nil.
func (l *ExecutionLogger) Reset() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.Reset()
}

// SetWriter sets the custom output writer. Safe to call on nil.
func (l *ExecutionLogger) SetWriter(w io.Writer) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writer = w
}

// GetWriter returns the custom output writer if configured, falling back to os.Stdout. Safe to call on nil.
func (l *ExecutionLogger) GetWriter() io.Writer {
	if l == nil {
		return os.Stdout
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.writer != nil {
		return l.writer
	}
	return os.Stdout
}

// Write implements io.Writer by appending data to the execution buffer. Safe to call on nil.
func (l *ExecutionLogger) Write(p []byte) (n int, err error) {
	if l == nil {
		return len(p), nil
	}
	l.Append(p)
	return len(p), nil
}

// WriteErrorLog writes a structured error log file containing session details and captured command output.
// If logger is nil or captured log is empty, a fallback placeholder is written.
func WriteErrorLog(
	logger *ExecutionLogger,
	dumpDir string,
	action string,
	details map[string]string,
	err error,
) (string, error) {
	if strings.TrimSpace(dumpDir) == "" {
		dumpDir = "dumps"
	}
	logsDir := filepath.Join(dumpDir, "logs")
	if mkErr := os.MkdirAll(logsDir, 0755); mkErr != nil {
		return "", fmt.Errorf("create logs directory %q: %w", logsDir, mkErr)
	}

	timestamp := time.Now().UTC().Format("20060102_150405")
	cleanAction := strings.ToLower(strings.TrimSpace(action))
	if cleanAction == "" {
		cleanAction = "operation"
	}
	logFileName := fmt.Sprintf("error_%s_%s.log", timestamp, cleanAction)
	logFilePath := filepath.Join(logsDir, logFileName)

	var sb strings.Builder
	sb.WriteString("================================================================================\n")
	sb.WriteString("STACKIT PostgreSQL CLI - Execution Error Log\n")
	sb.WriteString("================================================================================\n")
	sb.WriteString(fmt.Sprintf("Timestamp:   %s\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Action:      %s\n", action))

	for k, v := range details {
		sb.WriteString(fmt.Sprintf("%-12s %s\n", k+":", v))
	}

	if err != nil {
		sb.WriteString(fmt.Sprintf("Error:       %s\n", err.Error()))
	}
	sb.WriteString("================================================================================\n")
	sb.WriteString("Captured Command Output / Transcript:\n")
	sb.WriteString("================================================================================\n")

	var captured string
	if logger != nil {
		captured = logger.GetLog()
	}
	if strings.TrimSpace(captured) == "" {
		sb.WriteString("(No stdout/stderr captured during this session)\n")
	} else {
		sb.WriteString(captured)
		if !strings.HasSuffix(captured, "\n") {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("================================================================================\n")

	if writeErr := os.WriteFile(logFilePath, []byte(sb.String()), 0644); writeErr != nil {
		return "", fmt.Errorf("write error log %q: %w", logFilePath, writeErr)
	}

	return logFilePath, nil
}
