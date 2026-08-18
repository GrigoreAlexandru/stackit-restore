package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type StepStatus int

const (
	StepPending StepStatus = iota
	StepRunning
	StepCompleted
	StepCompletedWithWarnings
	StepFailed
)

var (
	iconCompleted = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")).Render("[✓]")
	iconWarning   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220")).Render("[!]")
	iconRunning   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Render("[>]")
	iconPending   = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("[ ]")
	iconFailed    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render("[✗]")

	styleTitleRunning   = lipgloss.NewStyle().Bold(true)
	styleTitleCompleted = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleTitleWarning   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	styleTitlePending   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleTitleFailed    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
)

type StepTracker struct {
	Title    string
	Steps    []string
	Statuses []StepStatus
	writer   io.Writer
}

func NewStepTracker(title string, steps []string) *StepTracker {
	statuses := make([]StepStatus, len(steps))
	for i := range statuses {
		statuses[i] = StepPending
	}
	return &StepTracker{
		Title:    title,
		Steps:    steps,
		Statuses: statuses,
		writer:   os.Stdout,
	}
}

func (st *StepTracker) SetWriter(w io.Writer) {
	st.writer = w
}

func (st *StepTracker) PrintHeader() {
	fmt.Fprintf(st.writer, "\n%s\n", strings.Repeat("=", 80))
	if st.Title != "" {
		fmt.Fprintf(st.writer, "%s\n", st.Title)
		fmt.Fprintf(st.writer, "%s\n", strings.Repeat("=", 80))
	}
	fmt.Fprintf(st.writer, "Planned Execution Steps (%d total):\n", len(st.Steps))
	for i, step := range st.Steps {
		fmt.Fprintf(st.writer, "  %s Step %d/%d: %s\n", iconPending, i+1, len(st.Steps), styleTitlePending.Render(step))
	}
	fmt.Fprintf(st.writer, "%s\n\n", strings.Repeat("-", 80))
}

func (st *StepTracker) StartStep(index int) {
	if index < 0 || index >= len(st.Statuses) {
		return
	}
	st.Statuses[index] = StepRunning
	fmt.Fprintf(
		st.writer,
		"%s Step %d/%d: %s...\n",
		iconRunning,
		index+1,
		len(st.Steps),
		styleTitleRunning.Render(st.Steps[index]),
	)
}

func (st *StepTracker) CompleteStep(index int) {
	if index < 0 || index >= len(st.Statuses) {
		return
	}
	st.Statuses[index] = StepCompleted
	fmt.Fprintf(
		st.writer,
		"%s Step %d/%d Completed: %s\n\n",
		iconCompleted,
		index+1,
		len(st.Steps),
		styleTitleCompleted.Render(st.Steps[index]),
	)
}

func (st *StepTracker) CompleteStepWithWarning(index int, warning string) {
	if index < 0 || index >= len(st.Statuses) {
		return
	}
	st.Statuses[index] = StepCompletedWithWarnings
	fmt.Fprintf(
		st.writer,
		"%s Step %d/%d Completed with Warnings: %s\n",
		iconWarning,
		index+1,
		len(st.Steps),
		styleTitleWarning.Render(st.Steps[index]),
	)
	if warning != "" {
		fmt.Fprintf(st.writer, "   Notice: %s\n", warning)
	}
	fmt.Fprintln(st.writer)
}

func (st *StepTracker) FailStep(index int, err error) {
	if index >= 0 && index < len(st.Statuses) {
		st.Statuses[index] = StepFailed
	}
	fmt.Fprintf(
		st.writer,
		"\n%s Step %d/%d Failed: %s\n",
		iconFailed,
		index+1,
		len(st.Steps),
		styleTitleFailed.Render(st.Steps[index]),
	)
	if err != nil {
		fmt.Fprintf(st.writer, "   Error: %s\n", err.Error())
	}
	fmt.Fprintln(st.writer)
}

func (st *StepTracker) RenderSummary() {
	fmt.Fprintf(st.writer, "%s\n", strings.Repeat("=", 80))
	fmt.Fprintf(st.writer, "Execution Summary:\n")
	for i, step := range st.Steps {
		var icon string
		var styledTitle string
		switch st.Statuses[i] {
		case StepCompleted:
			icon = iconCompleted
			styledTitle = styleTitleCompleted.Render(step)
		case StepCompletedWithWarnings:
			icon = iconWarning
			styledTitle = styleTitleWarning.Render(step)
		case StepRunning:
			icon = iconRunning
			styledTitle = styleTitleRunning.Render(step)
		case StepFailed:
			icon = iconFailed
			styledTitle = styleTitleFailed.Render(step)
		default:
			icon = iconPending
			styledTitle = styleTitlePending.Render(step)
		}
		fmt.Fprintf(st.writer, "  %s Step %d/%d: %s\n", icon, i+1, len(st.Steps), styledTitle)
	}
	fmt.Fprintf(st.writer, "%s\n\n", strings.Repeat("=", 80))
}
