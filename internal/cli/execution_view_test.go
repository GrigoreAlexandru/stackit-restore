package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestExecutionModel_TransitionsAndRendering(t *testing.T) {
	steps := []string{
		"Extract live database dump",
		"Restore dump into target database",
	}

	subChan := make(chan tea.Msg, 10)
	model := newExecutionModel("Test Sync Execution", steps, subChan)

	// Initial render
	view := model.View()
	if !strings.Contains(view, "Test Sync Execution") {
		t.Errorf("expected view to contain title, got:\n%s", view)
	}
	if !strings.Contains(view, "Step 1/2: Extract live database dump") {
		t.Errorf("expected step 1 in view, got:\n%s", view)
	}

	// Update window size
	m, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = m.(executionModel)

	// Step 1 start
	m, _ = model.Update(msgStepStart{index: 0})
	model = m.(executionModel)
	if model.statuses[0] != StepRunning {
		t.Errorf("expected step 0 to be running")
	}

	// Output line arrival
	m, _ = model.Update(msgOutputLine{line: "$ pg_dump --verbose app_prod"})
	model = m.(executionModel)
	if len(model.outputLines) != 1 {
		t.Errorf("expected 1 output line, got %d", len(model.outputLines))
	}

	// Step 1 complete & Step 2 start
	m, _ = model.Update(msgStepComplete{index: 0})
	model = m.(executionModel)
	m, _ = model.Update(msgStepStart{index: 1})
	model = m.(executionModel)

	// Step 2 warning
	m, _ = model.Update(msgStepWarning{index: 1, warning: "ignored missing extensions"})
	model = m.(executionModel)
	if model.statuses[1] != StepCompletedWithWarnings {
		t.Errorf("expected step 1 to be completed with warnings")
	}

	// Step 2 fail
	testErr := errors.New("restore failed")
	m, _ = model.Update(msgStepFail{index: 1, err: testErr})
	model = m.(executionModel)
	if model.statuses[1] != StepFailed {
		t.Errorf("expected step 1 to be failed")
	}

	// Execution done
	m, cmd := model.Update(msgExecutionDone{err: testErr})
	model = m.(executionModel)
	if !model.done {
		t.Errorf("expected model to be done")
	}
	if cmd == nil {
		t.Errorf("expected tea.Quit command upon execution done")
	}
}

func TestRunWithStepView_NonTTY(t *testing.T) {
	steps := []string{"Step 1", "Step 2"}
	details := map[string]string{"Action": "test"}

	executed := false
	err := RunWithStepView(
		context.Background(),
		"Non-TTY Test",
		steps,
		"test",
		details,
		func(ctx context.Context, reporter StepReporter) error {
			reporter.StartStep(0)
			reporter.CompleteStep(0)
			reporter.StartStep(1)
			reporter.CompleteStep(1)
			executed = true
			return nil
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Errorf("expected worker function to execute")
	}
}
