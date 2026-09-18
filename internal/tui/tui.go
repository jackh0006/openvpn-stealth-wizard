package tui

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

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

func init() {
	_ = spinner.Dot
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213")).
			Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 2)
	whyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true)
	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	selStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
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
	stage      stage
	modeIdx    int
	cfg        cfg.Config
	inputs     []textinput.Model
	focus      int
	fieldErrs  [6]string
	errMsg     string
	detectNote string
	portWarn   string
	log        *logx.Logger
	viewport   viewport.Model
	spinner    spinner.Model
	steps      []steps.Step
	cur        int
	cancelAsk  bool
	cancelled  bool
	results    []check.Item
}

type logTickMsg struct{}
type stepDoneMsg struct{ idx int }
type runDoneMsg struct{}
type detectIPMsg struct{ ip string }

func fieldLabels() []string {
	return []string{"Domain or IP", "Fallback IP", "Port", "VPN username", "VPN password", "Email"}
}

func New() Model {
	c := cfg.Defaults()
	c.Mode = cfg.ModeDomain
	if last, ok := cfg.LoadLast(); ok {
		if last.Host != "" {
			c = last
		}
	}
	vals := []string{c.Host, c.Fallback, strconv.Itoa(c.Port), c.VPNUser, "", "admin@example.com"}
	if c.Host == "" {
		vals[0] = "vpn.example.com"
	}
	if c.Email != "" {
		vals[5] = c.Email
	}
	inputs := make([]textinput.Model, 6)
	for i := range inputs {
		ti := textinput.New()
		ti.Placeholder = fieldLabels()[i]
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
	return Model{stage: stageMode, cfg: c, inputs: inputs, log: logx.New(), spinner: sp}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, detectPublicIP)
}

func detectPublicIP() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for _, url := range []string{"https://ifconfig.me", "https://api.ipify.org"} {
		if out, err := exec.CommandContext(ctx, "curl", "-s", "--max-time", "6", url).Output(); err == nil {
			if ip := strings.TrimSpace(string(out)); ip != "" && !strings.Contains(ip, "<") {
				return detectIPMsg{ip: ip}
			}
		}
	}
	return detectIPMsg{}
}

func (m Model) running() bool { return m.stage == stageRun }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case detectIPMsg:
		if msg.ip != "" && strings.TrimSpace(m.inputs[1].Value()) == "" {
			m.inputs[1].SetValue(msg.ip)
			m.detectNote = "detected this server IP for the DNS-bypass fallback: " + msg.ip
		}
		return m, nil
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
		if m.cancelled {
			m.stage = stageDone
			m.results = []check.Item{{Name: "run cancelled", OK: false, Detail: "stopped by user; completed steps kept"}}
			return m, nil
		}
		m.cur = msg.idx + 1
		if m.cur < len(m.steps) {
			return m, m.execStep(m.cur)
		}
		return m, func() tea.Msg { return runDoneMsg{} }
	case runDoneMsg:
		m.results = check.All(m.cfg)
		m.stage = stageDone
		return m, nil
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
		if r := s.Check(m.cfg); r.OK && m.modeIdx == 2 {
			m.log.OK("already good: " + r.Detail + " (skipped)")
			return stepDoneMsg{idx: idx}
		}
		if err := s.Apply(context.Background(), m.cfg, m.log); err != nil {
			m.log.Fail(s.Title() + ": " + err.Error())
		} else {
			m.log.OK(s.Title() + " done")
		}
		return stepDoneMsg{idx: idx}
	}
}

func (m *Model) applyForm() {
	c := m.cfg
	c.Host = strings.TrimSpace(m.inputs[0].Value())
	c.Fallback = strings.TrimSpace(m.inputs[1].Value())
	if p, err := strconv.Atoi(strings.TrimSpace(m.inputs[2].Value())); err == nil {
		c.Port = p
	} else {
		c.Port = -1
	}
	c.VPNUser = strings.TrimSpace(m.inputs[3].Value())
	c.VPNPass = m.inputs[4].Value()
	c.Email = strings.TrimSpace(m.inputs[5].Value())
	m.cfg = c
}

// validateForm maps problems onto individual fields.
func (m *Model) validateForm() bool {
	m.fieldErrs = [6]string{}
	m.errMsg = ""
	c := m.cfg
	bad := func(i int, msg string) {
		m.fieldErrs[i] = msg
	}
	if c.Mode == cfg.ModeDomain {
		if strings.Contains(c.Host, " ") || !strings.Contains(c.Host, ".") {
			bad(0, "does not look like a domain")
		}
		if c.Fallback != "" && !validIP(c.Fallback) {
			bad(1, "not an IP address")
		}
		if !strings.Contains(c.Email, "@") {
			bad(5, "email needed for the free certificate")
		}
	} else {
		if !validIP(c.Host) {
			bad(0, "mode is IP but this is not an IP")
		}
	}
	if c.Port < 1 || c.Port > 65535 {
		bad(2, "use 1-65535 (443 recommended)")
	}
	if strings.TrimSpace(c.VPNUser) == "" {
		bad(3, "username is empty")
	}
	if len(c.VPNPass) < 8 {
		bad(4, "at least 8 characters")
	}
	for _, e := range m.fieldErrs {
		if e != "" {
			m.errMsg = "fix the marked fields, then press enter"
			return false
		}
	}
	return true
}

func validIP(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return false
		}
	}
	return true
}

func (m Model) updateInputs(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for i := range m.inputs {
		var cmd tea.Cmd
		m.inputs[i], cmd = m.inputs[i].Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
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
				m.results = check.All(m.cfg)
				m.stage = stageDone
				return m, nil
			}
			m.stage = stageForm
		case "esc", "q":
			return m, tea.Quit
		default:
			return m, nil
		}
	case stageForm:
		switch k {
		case "tab", "down":
			m.inputs[m.focus].Blur()
			m.focus = (m.focus + 1) % len(m.inputs)
			m.inputs[m.focus].Focus()
			return m, nil
		case "shift+tab", "up":
			m.inputs[m.focus].Blur()
			m.focus = (m.focus + len(m.inputs) - 1) % len(m.inputs)
			m.inputs[m.focus].Focus()
			return m, nil
		case "ctrl+d":
			if m.cfg.Mode == cfg.ModeDomain {
				m.cfg.Mode = cfg.ModeIP
			} else {
				m.cfg.Mode = cfg.ModeDomain
			}
			return m, nil
		case "enter":
			if m.focus < len(m.inputs)-1 {
				m.inputs[m.focus].Blur()
				m.focus++
				m.inputs[m.focus].Focus()
				return m, nil
			}
			m.applyForm()
			if !m.validateForm() {
				return m, nil
			}
			m.cfg.SaveLast()
			if r := (steps.Conflicts{}.Check(m.cfg)); !r.OK {
				m.portWarn = r.Detail + " — change the port or resolve it first"
			} else {
				m.portWarn = ""
			}
			m.stage = stageReview
			return m, nil
		case "esc":
			m.stage = stageMode
			return m, nil
		}
		return m.updateInputs(msg)
	case stageReview:
		switch k {
		case "enter", "y":
			m.steps = steps.All()
			m.cur = 0
			m.cancelAsk = false
			m.cancelled = false
			m.viewport = viewport.New(100, 20)
			m.stage = stageRun
			return m, tea.Batch(waitLog(m.log), m.execStep(0))
		case "esc", "n":
			m.stage = stageForm
		}
	case stageRun:
		if k == "esc" {
			if m.cancelAsk {
				m.cancelled = true
				m.cancelAsk = false
				m.log.Fail("stop requested — finishing current step…")
				return m, nil
			}
			m.cancelAsk = true
			return m, nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	case stageDone:
		switch k {
		case "enter", "q":
			return m, tea.Quit
		case "esc":
			m.cancelAsk = false
			m.stage = stageMode
		}
	}
	return m, nil
}

func (m Model) View() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render(banner) + "\n\n")
	back := helpStyle.Render("esc back • ctrl+c quit")
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
		sb.WriteString(helpStyle.Render("\n↑/↓ choose • enter confirm • ") + back)
	case stageForm:
		sb.WriteString(fmt.Sprintf("Setup inputs  (mode: %s, ctrl+d switches ip/domain)\n", m.cfg.Mode))
		if m.detectNote != "" {
			sb.WriteString(warnStyle.Render("★ "+m.detectNote) + "\n")
		}
		sb.WriteString("\n")
		labels := fieldLabels()
		for i := range m.inputs {
			sb.WriteString(labels[i] + "\n" + m.inputs[i].View() + "\n")
			if m.fieldErrs[i] != "" {
				sb.WriteString(errStyle.Render("  ⚠ "+m.fieldErrs[i]) + "\n")
			}
			sb.WriteString("\n")
		}
		if m.errMsg != "" {
			sb.WriteString(errStyle.Render("⚠ "+m.errMsg) + "\n\n")
		}
		sb.WriteString(helpStyle.Render("type to edit • tab next field • enter continue • ") + back)
	case stageReview:
		sb.WriteString("Plan preview — I will do exactly this:\n\n")
		for i, s := range steps.All() {
			sb.WriteString(fmt.Sprintf("  %d. %s\n     %s\n", i+1, s.Title(), whyStyle.Render(s.Why())))
		}
		sb.WriteString(fmt.Sprintf("\nTarget: %s %s:%d → %s\n", m.cfg.Mode, m.cfg.Host, m.cfg.Port, m.cfg.OutDir))
		if m.portWarn != "" {
			sb.WriteString(warnStyle.Render("⚠ "+m.portWarn) + "\n")
		}
		sb.WriteString(helpStyle.Render("\nenter/y start • ") + back)
	case stageRun:
		if m.cancelAsk {
			sb.WriteString(errStyle.Render("Press esc again to stop after this step. ") + "\n\n")
		}
		sb.WriteString(m.spinner.View() + fmt.Sprintf(" working… step %d/%d (esc to stop)\n\n", min(m.cur+1, len(m.steps)), len(m.steps)))
		sb.WriteString(m.viewport.View())
	case stageDone:
		sb.WriteString("Health check:\n\n")
		for _, r := range m.results {
			mark := "✔"
			if !r.OK {
				mark = "✘"
			}
			sb.WriteString(fmt.Sprintf("  %s %-28s %s\n", mark, r.Name, whyStyle.Render(r.Detail)))
		}
		sb.WriteString("\n" + selStyle.Render("Done. Import the .ovpn, user "+m.cfg.VPNUser+"."))
		sb.WriteString(helpStyle.Render("\nenter quit • ") + back)
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
