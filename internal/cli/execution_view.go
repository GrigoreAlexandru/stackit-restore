package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/postgres"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

type StepReporter interface {
	StartStep(index int)
	CompleteStep(index int)
	CompleteStepWithWarning(index int, warning string)
	FailStep(index int, err error)
}

type msgStepStart struct {
	index int
}

type msgStepComplete struct {
	index int
}

type msgStepWarning struct {
	index   int
	warning string
}

type msgStepFail struct {
	index int
	err   error
}

type msgOutputLine struct {
	line string
}

type msgExecutionDone struct {
	err error
}

type executionModel struct {
	title       string
	steps       []string
	statuses    []StepStatus
	viewport    viewport.Model
	outputLines []string
	width       int
	height      int
	ready       bool
	done        bool
	err         error
	subChan     chan tea.Msg
}

func newExecutionModel(title string, steps []string, subChan chan tea.Msg) executionModel {
	statuses := make([]StepStatus, len(steps))
	for i := range statuses {
		statuses[i] = StepPending
	}
	return executionModel{
		title:       title,
		steps:       steps,
		statuses:    statuses,
		outputLines: make([]string, 0, 100),
		width:       80,
		height:      24,
		subChan:     subChan,
	}
}

func (m executionModel) Init() tea.Cmd {
	return waitForMsg(m.subChan)
}

func waitForMsg(sub chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-sub
		if !ok {
			return nil
		}
		return msg
	}
}

func (m executionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg == nil {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := 10
		if m.height > 25 {
			vpHeight = 12
		}
		vpWidth := m.width - 4
		if vpWidth > 100 {
			vpWidth = 100
		}
		if vpWidth < 40 {
			vpWidth = 40
		}
		if !m.ready {
			m.viewport = viewport.New(vpWidth, vpHeight)
			m.ready = true
		} else {
			m.viewport.Width = vpWidth
			m.viewport.Height = vpHeight
		}
		return m, waitForMsg(m.subChan)

	case msgStepStart:
		if msg.index >= 0 && msg.index < len(m.statuses) {
			m.statuses[msg.index] = StepRunning
		}
		return m, waitForMsg(m.subChan)

	case msgStepComplete:
		if msg.index >= 0 && msg.index < len(m.statuses) {
			m.statuses[msg.index] = StepCompleted
		}
		return m, waitForMsg(m.subChan)

	case msgStepWarning:
		if msg.index >= 0 && msg.index < len(m.statuses) {
			m.statuses[msg.index] = StepCompletedWithWarnings
		}
		return m, waitForMsg(m.subChan)

	case msgStepFail:
		if msg.index >= 0 && msg.index < len(m.statuses) {
			m.statuses[msg.index] = StepFailed
		}
		return m, waitForMsg(m.subChan)

	case msgOutputLine:
		m.outputLines = append(m.outputLines, msg.line)
		if len(m.outputLines) > 500 {
			m.outputLines = m.outputLines[len(m.outputLines)-500:]
		}
		if !m.ready {
			m.viewport = viewport.New(80, 10)
			m.ready = true
		}
		m.viewport.SetContent(strings.Join(m.outputLines, "\n"))
		m.viewport.GotoBottom()
		return m, waitForMsg(m.subChan)

	case msgExecutionDone:
		m.done = true
		m.err = msg.err
		return m, tea.Quit
	}

	return m, waitForMsg(m.subChan)
}

func (m executionModel) View() string {
	var sb strings.Builder

	sb.WriteString("\n" + strings.Repeat("=", 80) + "\n")
	if m.title != "" {
		sb.WriteString(m.title + "\n")
		sb.WriteString(strings.Repeat("=", 80) + "\n")
	}
	sb.WriteString(fmt.Sprintf("Planned Execution Steps (%d total):\n", len(m.steps)))
	for i, step := range m.steps {
		var icon string
		var styledTitle string
		switch m.statuses[i] {
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
		sb.WriteString(fmt.Sprintf("  %s Step %d/%d: %s\n", icon, i+1, len(m.steps), styledTitle))
	}
	sb.WriteString(strings.Repeat("-", 80) + "\n")

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)

	var content string
	if m.ready {
		content = m.viewport.View()
	}
	if strings.TrimSpace(content) == "" {
		content = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Waiting for command output...")
	}

	sb.WriteString(titleStyle.Render("Command Output (Live):") + "\n")
	sb.WriteString(boxStyle.Render(content) + "\n")

	return sb.String()
}

type channelReporter struct {
	subChan chan tea.Msg
}

func (r *channelReporter) StartStep(index int) {
	r.subChan <- msgStepStart{index: index}
}

func (r *channelReporter) CompleteStep(index int) {
	r.subChan <- msgStepComplete{index: index}
}

func (r *channelReporter) CompleteStepWithWarning(index int, warning string) {
	r.subChan <- msgStepWarning{index: index, warning: warning}
}

func (r *channelReporter) FailStep(index int, err error) {
	r.subChan <- msgStepFail{index: index, err: err}
}

type channelLogWriter struct {
	subChan chan tea.Msg
	buf     strings.Builder
	mu      sync.Mutex
}

func (w *channelLogWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, b := range p {
		if b == '\n' {
			line := w.buf.String()
			w.buf.Reset()
			w.subChan <- msgOutputLine{line: line}
		} else {
			w.buf.WriteByte(b)
		}
	}
	return len(p), nil
}

func (w *channelLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len() > 0 {
		line := w.buf.String()
		w.buf.Reset()
		w.subChan <- msgOutputLine{line: line}
	}
}

// RunWithStepView runs fn with either a TUI step-view (when stdout is a TTY) or a
// linear streaming fallback. logger is injected so command output is captured and
// forwarded to the live display simultaneously.
func RunWithStepView(
	ctx context.Context,
	title string,
	steps []string,
	actionName string,
	contextDetails map[string]string,
	logger *postgres.ExecutionLogger,
	fn func(ctx context.Context, reporter StepReporter) error,
) error {
	if logger != nil {
		logger.Reset()
	}

	// Check if standard output is a TTY
	isTTY := isatty.IsTerminal(os.Stdout.Fd())

	if !isTTY {
		// Non-TTY fallback (linear streaming)
		tracker := NewStepTracker(title, steps)
		tracker.PrintHeader()
		err := fn(ctx, tracker)
		if err != nil {
			return handleExecutionError(actionName, contextDetails, tracker, 0, err, logger)
		}
		tracker.RenderSummary()
		return nil
	}

	subChan := make(chan tea.Msg, 200)
	model := newExecutionModel(title, steps, subChan)
	p := tea.NewProgram(model)

	reporter := &channelReporter{subChan: subChan}
	writer := &channelLogWriter{subChan: subChan}
	if logger != nil {
		logger.SetWriter(writer)
	}

	var workerErr error
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		defer func() {
			writer.Flush()
			if logger != nil {
				logger.SetWriter(nil)
			}
			subChan <- msgExecutionDone{err: workerErr}
		}()

		workerErr = fn(ctx, reporter)
	}()

	finalModel, pErr := p.Run()
	wg.Wait()

	if pErr != nil {
		// If Bubble Tea encountered an error running TUI, handle workerErr directly
		if workerErr != nil {
			return handleExecutionError(actionName, contextDetails, nil, 0, workerErr, logger)
		}
		return nil
	}

	m, ok := finalModel.(executionModel)
	if ok && m.err != nil {
		return handleExecutionError(actionName, contextDetails, nil, 0, m.err, logger)
	}
	if workerErr != nil {
		return handleExecutionError(actionName, contextDetails, nil, 0, workerErr, logger)
	}

	// Render the final clean checklist summary
	if ok {
		summaryTracker := NewStepTracker(title, steps)
		summaryTracker.Statuses = m.statuses
		summaryTracker.RenderSummary()
	}

	return nil
}
