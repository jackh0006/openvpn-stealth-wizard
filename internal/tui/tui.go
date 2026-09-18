package tui

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jackh0006/openvpn-stealth-wizard/internal/cfg"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/check"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/logx"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/steps"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213")).
			Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 2)
	whyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true)
	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	selStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
)

const banner = `
   ___  ___  ___ _  __ ___  ___ _  __
  / _ \/ _ \/ -_) |/ |/ _ \/ -_) |/ /
  \___/ .__/\__/|___/| .__/\__/|___/
      /_/           /_/   stealth wizard
`

type stage int

const (
	stageMode stage = iota
	stageForm
	stageReview
	stageRun
	stageVerify
	stageDone
)

type modeItem struct {
	name string
	desc string
}

var modes = []modeItem{
	{"install", "Fresh setup: builds everything step by step"},
	{"check", "Health check only: read-only, changes nothing"},
	{"fix", "Repair mode: re-applies missing pieces only"},
}

// Model is the wizard state machine.
type Model struct {
	stage    stage
	modeIdx  int
	cfg      cfg.Config
	inputs   []textinput.Model
	focus    int
	errMsg   string
	log      *logx.Logger
	viewport viewport.Model
	spinner  spinner.Model
	steps    []steps.Step
	cur      int
	results  []check.Item
	done     chan struct{}
}

type logTickMsg struct{}
type stepDoneMsg struct{ idx int }
type runDoneMsg struct{}

func New() Model {
	c := cfg.Defaults()
	c.Mode = cfg.ModeDomain
	inputs := make([]textinput.Model, 6)
	labels := []string{"Domain or IP", "Fallback IP (DNS bypass, optional)", "Port", "VPN username", "VPN password", "Email (domain mode)"}
	vals := []string{"vpn.example.com", "", "443", "alice", "", "admin@example.com"}
	for i := range inputs {
		ti := textinput.New()
		ti.Placeholder = labels[i]
		ti.SetValue(vals[i])
		if i == 4 {
			ti.EchoMode = textinput.EchoPassword
		}
		ti.CharLimit = 128
		inputs[i] = ti
	}
	inputs[0].Focus()
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	return Model{stage: stageMode, cfg: c, inputs: inputs, log: logx.New(), spinner: sp, done: make(chan struct{})}
}

func (m Model) Init() tea.Cmd { return m.spinner.Tick }

func (m Model) running() bool { return m.stage == stageRun }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case logTickMsg:
		m.viewport.SetContent(m.log.Tail(100))
		m.viewport.GotoBottom()
		if m.running() {
			return m, waitLog(m.log)
		}
		return m, nil
	case stepDoneMsg:
		m.cur = msg.idx + 1
		if m.cur < len(m.steps) {
			return m, m.execStep(m.cur)
		}
		return m, func() tea.Msg { return runDoneMsg{} }
	case runDoneMsg:
		m.stage = stageVerify
		m.results = check.All(m.cfg)
		m.stage = stageDone
		return m, nil
	}
	if m.stage == stageForm {
		var cmds []tea.Cmd
		for i := range m.inputs {
			var cmd tea.Cmd
			m.inputs[i], cmd = m.inputs[i].Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	if m.stage == stageRun {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func waitLog(l *logx.Logger) tea.Cmd {
	return func() tea.Msg {
		<-l.Changed()
		return logTickMsg{}
	}
}

func (m Model) execStep(idx int) tea.Cmd {
	return func() tea.Msg {
		s := m.steps[idx]
		m.log.StepTitle(idx+1, len(m.steps), s.Title())
		m.log.Dim(s.Why())
		if err := s.Apply(context.Background(), m.cfg, m.log); err != nil {
			m.log.Fail(s.Title() + ": " + err.Error())
		} else {
			m.log.OK(s.Title() + " done")
		}
		return stepDoneMsg{idx: idx}
	}
}

func (m Model) applyForm() {
	c := m.cfg
	c.Host = strings.TrimSpace(m.inputs[0].Value())
	c.Fallback = strings.TrimSpace(m.inputs[1].Value())
	if p, err := strconv.Atoi(strings.TrimSpace(m.inputs[2].Value())); err == nil {
		c.Port = p
	}
	c.VPNUser = strings.TrimSpace(m.inputs[3].Value())
	c.VPNPass = m.inputs[4].Value()
	c.Email = strings.TrimSpace(m.inputs[5].Value())
	m.cfg = c
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" || k == "q" && m.stage != stageForm {
		return m, tea.Quit
	}
	switch m.stage {
	case stageMode:
		switch k {
		case "up", "k":
			m.modeIdx = (m.modeIdx + len(modes) - 1) % len(modes)
		case "down", "j":
			m.modeIdx = (m.modeIdx + 1) % len(modes)
		case "enter":
			if modes[m.modeIdx].name == "check" {
				m.stage = stageVerify
				m.results = check.All(m.cfg)
				m.stage = stageDone
				return m, nil
			}
			m.stage = stageForm
		}
	case stageForm:
		switch k {
		case "tab", "down":
			m.inputs[m.focus].Blur()
			m.focus = (m.focus + 1) % len(m.inputs)
			m.inputs[m.focus].Focus()
		case "shift+tab", "up":
			m.inputs[m.focus].Blur()
			m.focus = (m.focus + len(m.inputs) - 1) % len(m.inputs)
			m.inputs[m.focus].Focus()
		case "ctrl+d":
			if m.cfg.Mode == cfg.ModeDomain {
				m.cfg.Mode = cfg.ModeIP
			} else {
				m.cfg.Mode = cfg.ModeDomain
			}
		case "enter":
			if m.focus < len(m.inputs)-1 {
				m.inputs[m.focus].Blur()
				m.focus++
				m.inputs[m.focus].Focus()
				return m, nil
			}
			m.applyForm()
			if err := m.cfg.Validate(); err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
			m.errMsg = ""
			m.stage = stageReview
		case "esc":
			m.stage = stageMode
		}
	case stageReview:
		switch k {
		case "enter", "y":
			m.steps = steps.All()
			m.viewport = viewport.New(100, 20)
			m.stage = stageRun
			cmds := []tea.Cmd{waitLog(m.log), m.execStep(0)}
			return m, tea.Batch(cmds...)
		case "esc", "n":
			m.stage = stageForm
		}
	case stageDone:
		if k == "enter" || k == "esc" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) View() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render(banner) + "\n\n")
	switch m.stage {
	case stageMode:
		sb.WriteString("What shall we do today?\n\n")
		for i, md := range modes {
			marker := "  "
			if i == m.modeIdx {
				marker = selStyle.Render("▸ ")
			}
			sb.WriteString(fmt.Sprintf("%s%s — %s\n", marker, md.name, whyStyle.Render(md.desc)))
		}
		sb.WriteString(helpStyle.Render("\n↑/↓ choose • enter confirm • q quit"))
	case stageForm:
		sb.WriteString(fmt.Sprintf("Setup inputs  (mode: %s, ctrl+d toggles ip/domain)\n\n", m.cfg.Mode))
		labels := []string{"Domain or IP", "Fallback IP", "Port", "VPN username", "VPN password", "Email"}
		for i := range m.inputs {
			sb.WriteString(labels[i] + "\n" + m.inputs[i].View() + "\n\n")
		}
		if m.errMsg != "" {
			sb.WriteString(errStyle.Render("⚠ "+m.errMsg) + "\n\n")
		}
		sb.WriteString(helpStyle.Render("tab next • enter continue • esc back"))
	case stageReview:
		sb.WriteString("Plan preview — I will do exactly this:\n\n")
		for i, s := range steps.All() {
			sb.WriteString(fmt.Sprintf("  %d. %s\n     %s\n", i+1, s.Title(), whyStyle.Render(s.Why())))
		}
		sb.WriteString(fmt.Sprintf("\nTarget: %s %s:%d → %s\n", m.cfg.Mode, m.cfg.Host, m.cfg.Port, m.cfg.OutDir))
		sb.WriteString(helpStyle.Render("\nenter/y start • n/esc back"))
	case stageRun:
		sb.WriteString(m.spinner.View() + fmt.Sprintf(" working… step %d/%d\n\n", min(m.cur+1, len(m.steps)), len(m.steps)))
		sb.WriteString(m.viewport.View())
	case stageVerify, stageDone:
		sb.WriteString("Health check:\n\n")
		for _, r := range m.results {
			mark := "✔"
			if !r.OK {
				mark = "✘"
			}
			sb.WriteString(fmt.Sprintf("  %s %-28s %s\n", mark, r.Name, whyStyle.Render(r.Detail)))
		}
		sb.WriteString("\n" + selStyle.Render("Done. Import the .ovpn, user "+m.cfg.VPNUser+"."))
		sb.WriteString(helpStyle.Render("\nenter quit"))
	}
	return sb.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RunHeadless executes install without a TTY (flags path).
func RunHeadless(w io.Writer, c cfg.Config, fixesOnly bool) int {
	log := logx.New()
	stepsList := steps.All()
	code := 0
	for i, s := range stepsList {
		fmt.Fprintf(w, "\n━━━ [%d/%d] %s ━━━\n%s\n", i+1, len(stepsList), s.Title(), s.Why())
		if fixesOnly {
			if r := s.Check(c); r.OK {
				fmt.Fprintf(w, "  ✔ already good: %s\n", r.Detail)
				continue
			}
		}
		if err := s.Apply(context.Background(), c, log); err != nil {
			fmt.Fprintf(w, "  ✘ FAILED: %v\n", err)
			code = 1
		}
		for _, ln := range log.Lines() {
			fmt.Fprintln(w, ln)
		}
		log = logx.New()
	}
	return code
}

// CheckHeadless prints the read-only report.
func CheckHeadless(w io.Writer, c cfg.Config) int {
	code := 0
	for _, r := range check.All(c) {
		mark := "✔"
		if !r.OK {
			mark = "✘"
			code = 1
		}
		fmt.Fprintf(w, "%s %-28s %s\n", mark, r.Name, r.Detail)
	}
	return code
}
