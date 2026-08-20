package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// AssertSuccess asserts that the CLI exited with status 0.
func (r *RunResult) AssertSuccess(t testing.TB) *RunResult {
	t.Helper()
	if r.ExitCode != 0 {
		t.Fatalf("Expected exit code 0 (success), got %d.\nCommand: %v\nStdout:\n%s\nStderr:\n%s",
			r.ExitCode, r.Args, r.Stdout, r.Stderr)
	}
	return r
}

// AssertFailure asserts that the CLI exited with a non-zero status.
func (r *RunResult) AssertFailure(t testing.TB) *RunResult {
	t.Helper()
	if r.ExitCode == 0 {
		t.Fatalf("Expected non-zero exit code (failure), got 0.\nCommand: %v\nStdout:\n%s\nStderr:\n%s",
			r.Args, r.Stdout, r.Stderr)
	}
	return r
}

// AssertExitCode asserts that the CLI exited with the specific expected code.
func (r *RunResult) AssertExitCode(t testing.TB, expected int) *RunResult {
	t.Helper()
	if r.ExitCode != expected {
		t.Fatalf("Expected exit code %d, got %d.\nCommand: %v\nStdout:\n%s\nStderr:\n%s",
			expected, r.ExitCode, r.Args, r.Stdout, r.Stderr)
	}
	return r
}

// AssertStdoutContains asserts that stdout contains the specified substring.
func (r *RunResult) AssertStdoutContains(t testing.TB, substring string) *RunResult {
	t.Helper()
	if !strings.Contains(r.Stdout, substring) {
		t.Fatalf("Expected stdout to contain %q, but it did not.\nCommand: %v\nStdout:\n%s\nStderr:\n%s",
			substring, r.Args, r.Stdout, r.Stderr)
	}
	return r
}

// AssertStdoutNotContains asserts that stdout does not contain the specified substring.
func (r *RunResult) AssertStdoutNotContains(t testing.TB, substring string) *RunResult {
	t.Helper()
	if strings.Contains(r.Stdout, substring) {
		t.Fatalf("Expected stdout NOT to contain %q, but it did.\nCommand: %v\nStdout:\n%s",
			substring, r.Args, r.Stdout)
	}
	return r
}

// AssertStderrContains asserts that stderr contains the specified substring.
func (r *RunResult) AssertStderrContains(t testing.TB, substring string) *RunResult {
	t.Helper()
	if !strings.Contains(r.Stderr, substring) {
		t.Fatalf("Expected stderr to contain %q, but it did not.\nCommand: %v\nStdout:\n%s\nStderr:\n%s",
			substring, r.Args, r.Stdout, r.Stderr)
	}
	return r
}

// AssertStderrNotContains asserts that stderr does not contain the specified substring.
func (r *RunResult) AssertStderrNotContains(t testing.TB, substring string) *RunResult {
	t.Helper()
	if strings.Contains(r.Stderr, substring) {
		t.Fatalf("Expected stderr NOT to contain %q, but it did.\nCommand: %v\nStderr:\n%s",
			substring, r.Args, r.Stderr)
	}
	return r
}

// AssertCombinedContains asserts that combined output (stdout + stderr) contains the substring.
func (r *RunResult) AssertCombinedContains(t testing.TB, substring string) *RunResult {
	t.Helper()
	if !strings.Contains(r.CombinedOutput, substring) {
		t.Fatalf("Expected combined output to contain %q, but it did not.\nCommand: %v\nCombined Output:\n%s",
			substring, r.Args, r.CombinedOutput)
	}
	return r
}

// AssertCombinedNotContains asserts that combined output does not contain the substring.
func (r *RunResult) AssertCombinedNotContains(t testing.TB, substring string) *RunResult {
	t.Helper()
	if strings.Contains(r.CombinedOutput, substring) {
		t.Fatalf("Expected combined output NOT to contain %q, but it did.\nCommand: %v\nCombined Output:\n%s",
			substring, r.Args, r.CombinedOutput)
	}
	return r
}

// AssertStdoutMatches asserts that stdout matches a regular expression pattern.
func (r *RunResult) AssertStdoutMatches(t testing.TB, pattern string) *RunResult {
	t.Helper()
	matched, err := regexp.MatchString(pattern, r.Stdout)
	if err != nil {
		t.Fatalf("Invalid regex pattern %q: %v", pattern, err)
	}
	if !matched {
		t.Fatalf("Expected stdout to match pattern %q, but it did not.\nStdout:\n%s", pattern, r.Stdout)
	}
	return r
}

// AssertStderrMatches asserts that stderr matches a regular expression pattern.
func (r *RunResult) AssertStderrMatches(t testing.TB, pattern string) *RunResult {
	t.Helper()
	matched, err := regexp.MatchString(pattern, r.Stderr)
	if err != nil {
		t.Fatalf("Invalid regex pattern %q: %v", pattern, err)
	}
	if !matched {
		t.Fatalf("Expected stderr to match pattern %q, but it did not.\nStderr:\n%s", pattern, r.Stderr)
	}
	return r
}

// AssertDumpFileExists asserts that at least one dump file matching the pattern exists in dumpsDir.
func (e *TestEnv) AssertDumpFileExists(t testing.TB, pattern string) string {
	t.Helper()
	dumps, err := e.ListDumpFiles()
	if err != nil {
		t.Fatalf("Failed to list dump files: %v", err)
	}

	for _, dump := range dumps {
		matched, _ := filepath.Match(pattern, filepath.Base(dump))
		if matched || strings.Contains(filepath.Base(dump), pattern) {
			return dump
		}
	}

	t.Fatalf("Expected dump file matching %q in %s, but found: %v", pattern, e.DumpsDir, dumps)
	return ""
}

// AssertDumpFileCount asserts the exact number of .dump files present in dumpsDir.
func (e *TestEnv) AssertDumpFileCount(t testing.TB, expected int) {
	t.Helper()
	dumps, err := e.ListDumpFiles()
	if err != nil {
		t.Fatalf("Failed to list dump files: %v", err)
	}
	if len(dumps) != expected {
		t.Fatalf("Expected %d dump file(s) in %s, but found %d: %v", expected, e.DumpsDir, len(dumps), dumps)
	}
}

// AssertErrorLogWritten asserts that an error log was created under <dumps>/logs/ for the action.
func (e *TestEnv) AssertErrorLogWritten(t testing.TB, action string) string {
	t.Helper()
	logs, err := e.ListErrorLogs()
	if err != nil {
		t.Fatalf("Failed to list error logs: %v", err)
	}

	for _, logFile := range logs {
		base := filepath.Base(logFile)
		if action == "" || strings.Contains(base, action) {
			content, err := os.ReadFile(logFile)
			if err != nil {
				t.Fatalf("Failed to read error log %s: %v", logFile, err)
			}
			if len(content) == 0 {
				t.Fatalf("Error log %s was created but is empty", logFile)
			}
			return logFile
		}
	}

	t.Fatalf("Expected error log for action %q in %s/logs/, but found: %v", action, e.DumpsDir, logs)
	return ""
}

// AssertNoFatalErrorLogs asserts that no error logs were generated.
func (e *TestEnv) AssertNoFatalErrorLogs(t testing.TB) {
	t.Helper()
	logs, err := e.ListErrorLogs()
	if err != nil {
		t.Fatalf("Failed to list error logs: %v", err)
	}
	if len(logs) > 0 {
		t.Fatalf("Expected zero error logs in %s/logs/, but found %d: %v", e.DumpsDir, len(logs), logs)
	}
}

// AssertToolInvoked asserts that a specific tool (e.g. pg_dump, pg_restore) was invoked at least once.
func (e *TestEnv) AssertToolInvoked(t testing.TB, toolName string) InvocationRecord {
	t.Helper()
	invocations, err := e.GetInvocations()
	if err != nil {
		t.Fatalf("Failed to read invocations: %v", err)
	}

	for _, inv := range invocations {
		if inv.Tool == toolName {
			return inv
		}
	}

	t.Fatalf("Expected %s to be invoked, but recorded invocations were: %v", toolName, invocations)
	return InvocationRecord{}
}

// AssertToolInvokedWithArg asserts that a tool was invoked with a specific command argument.
func (e *TestEnv) AssertToolInvokedWithArg(t testing.TB, toolName, expectedArg string) InvocationRecord {
	t.Helper()
	invocations, err := e.GetInvocations()
	if err != nil {
		t.Fatalf("Failed to read invocations: %v", err)
	}

	for _, inv := range invocations {
		if inv.Tool == toolName {
			for _, arg := range inv.Args {
				if arg == expectedArg || strings.Contains(arg, expectedArg) {
					return inv
				}
			}
		}
	}

	t.Fatalf("Expected %s to be invoked with argument containing %q, but recorded invocations were: %v",
		toolName, expectedArg, invocations)
	return InvocationRecord{}
}

// AssertToolNotInvoked asserts that a tool was NOT invoked.
func (e *TestEnv) AssertToolNotInvoked(t testing.TB, toolName string) {
	t.Helper()
	invocations, err := e.GetInvocations()
	if err != nil {
		t.Fatalf("Failed to read invocations: %v", err)
	}

	for _, inv := range invocations {
		if inv.Tool == toolName {
			t.Fatalf("Expected %s NOT to be invoked, but found invocation: %v", toolName, inv)
		}
	}
}

// AssertToolInvocationCount asserts the exact number of times a tool was called.
func (e *TestEnv) AssertToolInvocationCount(t testing.TB, toolName string, expected int) {
	t.Helper()
	invocations, err := e.GetInvocations()
	if err != nil {
		t.Fatalf("Failed to read invocations: %v", err)
	}

	count := 0
	for _, inv := range invocations {
		if inv.Tool == toolName {
			count++
		}
	}

	if count != expected {
		t.Fatalf("Expected %s to be invoked %d time(s), but was invoked %d time(s): %v",
			toolName, expected, count, invocations)
	}
}
