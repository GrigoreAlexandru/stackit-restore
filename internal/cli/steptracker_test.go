package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestStepTracker(t *testing.T) {
	steps := []string{
		"Extract live database dump",
		"Restore dump into destination database",
	}

	var buf bytes.Buffer
	tracker := NewStepTracker("Test Sync Operation", steps)
	tracker.SetWriter(&buf)

	// Initial header
	tracker.PrintHeader()
	out := buf.String()
	if !strings.Contains(out, "Test Sync Operation") {
		t.Errorf("expected header to contain title, got: %s", out)
	}
	if !strings.Contains(out, "Step 1/2: Extract live database dump") {
		t.Errorf("expected step 1 to be listed, got: %s", out)
	}

	// Step 1 start & complete
	buf.Reset()
	tracker.StartStep(0)
	tracker.CompleteStep(0)
	out = buf.String()
	if !strings.Contains(out, "Step 1/2 Completed:") {
		t.Errorf("expected step 1 completion, got: %s", out)
	}

	// Step 2 start & complete with warnings
	buf.Reset()
	tracker.StartStep(1)
	tracker.CompleteStepWithWarning(1, "missing pg_stat_kcache extension ignored")
	out = buf.String()
	if !strings.Contains(out, "Step 2/2 Completed with Warnings:") || !strings.Contains(out, "missing pg_stat_kcache") {
		t.Errorf("expected step 2 warning notice, got: %s", out)
	}

	// Step 2 fail check
	buf.Reset()
	tracker.FailStep(1, errors.New("connection timeout"))
	out = buf.String()
	if !strings.Contains(out, "Step 2/2 Failed:") || !strings.Contains(out, "connection timeout") {
		t.Errorf("expected step 2 failure with error message, got: %s", out)
	}

	// Summary
	buf.Reset()
	tracker.RenderSummary()
	out = buf.String()
	if !strings.Contains(out, "Execution Summary:") {
		t.Errorf("expected execution summary header, got: %s", out)
	}
}
