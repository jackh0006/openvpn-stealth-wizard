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
	"github.com/jackh0006/openvpn-stealth-wizard/internal/manage"
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
	stageManage
	stagePrompt
)

type modeItem struct {
	name string
	desc string
}

var modes = []modeItem{
	{"install", "Fresh setup: builds everything step by step"},
	{"check", "Health check only: read-only, changes nothing"},
	{"fix", "Repair mode: re-applies missing pieces only"},
	{"manage", "Manage servers: users, restart, delete, logs"},
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
	mgServers  []manage.Server
	mgIdx      int
	mgMsg      string
	mgView     mgView
	prompt     promptState
}

type mgView int

const (
	mgMenu mgView = iota
	mgLogs
)

type logFollowTickMsg struct{}

// promptState is a reusable single-line question (username, password,
// delete confirmation) used by manage mode.
type promptState struct {
	active bool
	title  string
	hide   bool
	input  textinput.Model
	action string // add-user, set-pass, del-user, del-server, revoke
	arg    string // extra context (e.g. username for set-pass)
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
	case logFollowTickMsg:
		m.viewport.SetContent(manage.TailLog(200))
		m.viewport.GotoBottom()
		if m.stage == stageManage && m.mgView == mgLogs {
			return m, logFollowTick()
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
	case freePortMsg:
		if msg.err != nil {
			m.portWarn = "could not free port: " + msg.err.Error()
		} else {
			m.portWarn = ""
			if msg.wantRun {
				m.steps = steps.All()
				m.cur = 0
				m.cancelAsk = false
				m.cancelled = false
				m.viewport = viewport.New(100, 20)
				m.stage = stageRun
				return m, tea.Batch(waitLog(m.log), m.execStep(0))
			}
			m.portWarn = "port is free now — press enter to start install"
		}
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

func logFollowTick() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(1500 * time.Millisecond)
		return logFollowTickMsg{}
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
	if !c.NoPassword && len(c.VPNPass) < 8 {
		bad(4, "at least 8 characters (or ctrl+n for cert-only)")
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
			switch modes[m.modeIdx].name {
			case "check":
				m.results = check.All(m.cfg)
				m.stage = stageDone
				return m, nil
			case "manage":
				m.mgServers = manage.Discover()
				m.mgIdx = 0
				m.mgMsg = ""
				m.stage = stageManage
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
		case "ctrl+g":
			// Suggest a strong password for the focused password field.
			if m.focus == 4 {
				p := suggestPassword()
				m.inputs[4].SetValue(p)
				m.cfg.NoPassword = false
				m.detectNote = "suggested password filled — you can keep it or type your own"
			}
			return m, nil
		case "ctrl+n":
			m.cfg.NoPassword = !m.cfg.NoPassword
			if m.cfg.NoPassword {
				m.inputs[4].SetValue("")
				m.detectNote = "cert-only mode: no password will be required"
			} else {
				m.detectNote = "password mode: set a password (ctrl+g to suggest one)"
			}
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
			// Auto-offer suggested password if empty/insecure at final submit.
			if len(m.cfg.VPNPass) < 8 {
				m.inputs[4].SetValue(suggestPassword())
				m.applyForm()
				m.detectNote = "filled a strong password for you — press enter again to confirm"
				m.fieldErrs = [6]string{}
				m.errMsg = ""
				m.focus = 4
				m.inputs[4].Focus()
				return m, nil
			}
			m.cfg.SaveLast()
			if r := (steps.Conflicts{}.Check(m.cfg)); !r.OK {
				owner := steps.PortOwner(m.cfg.Port)
				if owner.Process == "openvpn" || owner.Process == "openvpn-server" || owner.Process == "unknown process" || owner.Unit != "" {
					m.portWarn = r.Detail + " — press f to free it (always asks before touching it)"
				} else {
					m.portWarn = r.Detail + " — press f to free it (files kept, service disabled)"
				}
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
			// Guard: port still taken? Ask before freeing, then continue.
			if m.portWarn != "" {
				o := steps.PortOwner(m.cfg.Port)
				m.prompt = promptState{
					active: true,
					title:  fmt.Sprintf("Port %d is held by %s. Free it now? (y/n)", m.cfg.Port, o.String()),
					action: "confirm-free-port",
					arg:    "",
				}
				ti := textinput.New()
				ti.Placeholder = "y or n"
				ti.CharLimit = 4
				ti.Focus()
				m.prompt.input = ti
				m.stage = stagePrompt
				return m, nil
			}
			m.steps = steps.All()
			m.cur = 0
			m.cancelAsk = false
			m.cancelled = false
			m.viewport = viewport.New(100, 20)
			m.stage = stageRun
			return m, tea.Batch(waitLog(m.log), m.execStep(0))
		case "f":
			if m.portWarn != "" {
				o := steps.PortOwner(m.cfg.Port)
				m.prompt = promptState{
					active: true,
					title:  fmt.Sprintf("Free port %d held by %s? (y/n)", m.cfg.Port, o.String()),
					action: "confirm-free-port-then-run",
					arg:    "",
				}
				ti := textinput.New()
				ti.Placeholder = "y or n"
				ti.CharLimit = 4
				ti.Focus()
				m.prompt.input = ti
				m.stage = stagePrompt
				return m, nil
			}
			return m, nil
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
	case stageManage:
		return m.handleManageKey(k)
	case stagePrompt:
		return m.handlePromptKey(k, msg)
	}
	return m, nil
}

type freePortMsg struct {
	err     error
	wantRun bool
}

func (m Model) freePortCmd() tea.Cmd { return m.freePortCmdWithFollow(false) }

func (m Model) freePortCmdWithFollow(wantRun bool) tea.Cmd {
	return func() tea.Msg {
		log := logx.New()
		err := steps.FreePort(context.Background(), m.cfg.Port, log)
		_ = log
		return freePortMsg{err: err, wantRun: wantRun}
	}
}

// mgCurrent returns the selected server, if any.
func (m Model) mgCurrent() (manage.Server, bool) {
	if len(m.mgServers) == 0 || m.mgIdx < 0 || m.mgIdx >= len(m.mgServers) {
		return manage.Server{}, false
	}
	return m.mgServers[m.mgIdx], true
}

func (m Model) refreshManage(msg string) Model {
	m.mgServers = manage.Discover()
	if m.mgIdx >= len(m.mgServers) {
		m.mgIdx = len(m.mgServers) - 1
	}
	if m.mgIdx < 0 {
		m.mgIdx = 0
	}
	m.mgMsg = msg
	return m
}

func (m Model) askPrompt(title, action, arg string, hide bool) Model {
	ti := textinput.New()
	ti.Placeholder = title
	ti.CharLimit = 128
	if hide {
		ti.EchoMode = textinput.EchoPassword
	}
	ti.Focus()
	m.prompt = promptState{active: true, title: title, hide: hide, input: ti, action: action, arg: arg}
	m.stage = stagePrompt
	return m
}

func (m Model) handleManageKey(k string) (tea.Model, tea.Cmd) {
	if m.mgView == mgLogs {
		m.mgView = mgMenu
		m.mgMsg = ""
		return m, nil
	}
	switch k {
	case "up", "k":
		if m.mgIdx > 0 {
			m.mgIdx--
		}
		m.mgMsg = ""
	case "down", "j":
		if m.mgIdx < len(m.mgServers)-1 {
			m.mgIdx++
		}
		m.mgMsg = ""
	case "1", "r":
		if s, ok := m.mgCurrent(); ok {
			if err := manage.Restart(s.Name); err != nil {
				m.mgMsg = "restart failed: " + err.Error()
			} else {
				m.mgMsg = s.Name + " restarted"
			}
			return m.refreshManage(m.mgMsg), nil
		}
	case "2", "c":
		if s, ok := m.mgCurrent(); ok {
			cls := manage.ConnectedClients(s.Name)
			if len(cls) == 0 {
				m.mgMsg = s.Name + ": no clients connected"
			} else {
				m.mgMsg = s.Name + " clients: " + strings.Join(cls, ", ")
			}
		}
	case "3", "l":
		m.mgView = mgLogs
		m.viewport = viewport.New(100, 20)
		m.viewport.SetContent(manage.TailLog(200))
		return m, logFollowTick()
	case "4", "u":
		users := manage.ListUsers()
		if len(users) == 0 {
			m.mgMsg = "no VPN users yet — press 5 to add one"
		} else {
			m.mgMsg = "users: " + strings.Join(users, ", ")
		}
	case "5", "a":
		return m.askPrompt("new username", "add-user", "", false), nil
	case "6", "p":
		return m.askPrompt("username to set password for", "set-pass-u", "", false), nil
	case "7", "d":
		return m.askPrompt("username to delete", "del-user", "", false), nil
	case "8", "v":
		return m.askPrompt("client certificate name to revoke", "revoke", "", false), nil
	case "9", "x":
		if s, ok := m.mgCurrent(); ok {
			return m.askPrompt("type "+s.Name+" to delete this server FOREVER (backup kept)", "del-server", s.Name, false), nil
		}
	case "0", "esc", "q":
		m.stage = stageMode
	}
	return m, nil
}

func (m Model) handlePromptKey(k string, msg tea.Msg) (tea.Model, tea.Cmd) {
	// Special handling for port-free confirmation prompt (used from review).
	if m.prompt.action == "confirm-free-port" || m.prompt.action == "confirm-free-port-then-run" {
		switch k {
		case "esc":
			m.prompt = promptState{}
			m.stage = stageReview
			return m, nil
		case "enter":
			val := strings.TrimSpace(strings.ToLower(m.prompt.input.Value()))
			act := m.prompt.action
			m.prompt = promptState{}
			m.stage = stageReview
			if val == "y" || val == "yes" {
				return m, m.freePortCmdWithFollow(act == "confirm-free-port-then-run")
			}
			m.portWarn = "port still taken — free it with f or change the port"
			return m, nil
		}
		var cmd tea.Cmd
		m.prompt.input, cmd = m.prompt.input.Update(msg)
		return m, cmd
	}
	switch k {
	case "esc":
		m.prompt = promptState{}
		m.stage = stageManage
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.prompt.input.Value())
		action, arg := m.prompt.action, m.prompt.arg
		m.prompt = promptState{}
		m.stage = stageManage
		switch action {
		case "add-user":
			if val == "" {
				m.mgMsg = "username empty, nothing done"
				return m, nil
			}
			pw := suggestPassword()
			m.mgMsg = "suggested password for " + val + ": " + pw + " — use p to change it, or a again to set this"
			return m.askPrompt("password for "+val+" (min 8 chars, ctrl+g to fill suggestion: "+pw+")", "add-pass", val, true), nil
		case "add-pass":
			if err := manage.SetUser(arg, val); err != nil {
				m.mgMsg = "add user failed: " + err.Error()
			} else {
				m.mgMsg = "user " + arg + " ready — download: scp root@YOUR_HOST:\"" + manage.BundleRoot + "/" + arg + ".ovpn\" ./"
			}
		case "set-pass-u":
			if val == "" {
				m.mgMsg = "username empty, nothing done"
				return m, nil
			}
			pw := suggestPassword()
			return m.askPrompt("new password for "+val+" (suggested: "+pw+")", "set-pass", val, true), nil
		case "set-pass":
			if err := manage.SetUser(arg, val); err != nil {
				m.mgMsg = "password change failed: " + err.Error()
			} else {
				m.mgMsg = "password changed for " + arg
			}
		case "del-user":
			if err := manage.DelUser(val); err != nil {
				m.mgMsg = "delete failed: " + err.Error()
			} else {
				m.mgMsg = "user " + val + " deleted"
			}
		case "del-server":
			if val != arg {
				m.mgMsg = "name did not match, server kept"
				return m.refreshManage(m.mgMsg), nil
			}
			if bak, err := manage.Delete(arg); err != nil {
				m.mgMsg = "delete failed: " + err.Error()
			} else {
				m.mgMsg = arg + " purged. Backup: " + bak
			}
		case "revoke":
			if out, err := manage.RevokeClient(val); err != nil {
				m.mgMsg = "revoke failed: " + err.Error()
			} else {
				m.mgMsg = val + ": " + out
			}
		}
		return m.refreshManage(m.mgMsg), nil
	}
	var cmd tea.Cmd
	m.prompt.input, cmd = m.prompt.input.Update(msg)
	return m, cmd
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
		pwLabel := "VPN password"
		if m.cfg.NoPassword {
			pwLabel = "VPN password (cert-only, disabled)"
		}
		sb.WriteString(fmt.Sprintf("Setup inputs  (mode: %s, ctrl+d switches ip/domain, ctrl+n toggles cert-only)\n", m.cfg.Mode))
		if m.detectNote != "" {
			sb.WriteString(warnStyle.Render("★ "+m.detectNote) + "\n")
		}
		sb.WriteString("\n")
		labels := fieldLabels()
		for i := range m.inputs {
			hint := ""
			if i == 4 {
				if m.cfg.NoPassword {
					hint = "  (cert-only — press ctrl+n to enable password)"
				} else {
					hint = "  (ctrl+g suggests a strong password, ctrl+n for cert-only)"
				}
			}
			lbl := labels[i]
			if i == 4 {
				lbl = pwLabel
			}
			sb.WriteString(lbl + hint + "\n" + m.inputs[i].View() + "\n")
			if m.fieldErrs[i] != "" {
				sb.WriteString(errStyle.Render("  ⚠ "+m.fieldErrs[i]) + "\n")
			}
			sb.WriteString("\n")
		}
		if m.errMsg != "" {
			sb.WriteString(errStyle.Render("⚠ "+m.errMsg) + "\n\n")
		}
		sb.WriteString(helpStyle.Render("type to edit • tab next field • ctrl+g suggest password • ctrl+n cert-only • enter continue • ") + back)
	case stageReview:
		sb.WriteString("Plan preview — I will do exactly this:\n\n")
		for i, s := range steps.All() {
			sb.WriteString(fmt.Sprintf("  %d. %s\n     %s\n", i+1, s.Title(), whyStyle.Render(s.Why())))
		}
		sb.WriteString(fmt.Sprintf("\nTarget: %s %s:%d → %s\n", m.cfg.Mode, m.cfg.Host, m.cfg.Port, m.cfg.OutDir))
		if m.portWarn != "" {
			sb.WriteString(warnStyle.Render("⚠ "+m.portWarn) + "\n")
		}
		sb.WriteString(helpStyle.Render("\nenter/y start • f free the port • ") + back)
	case stageManage:
		if m.mgView == mgLogs {
			sb.WriteString("Live logs — streaming (any key to go back)\n\n")
			sb.WriteString(m.viewport.View())
			break
		}
		sb.WriteString("Servers on this machine:\n\n")
		if len(m.mgServers) == 0 {
			sb.WriteString(whyStyle.Render("  none found under /etc/openvpn/server — install one first\n"))
		}
		for i, s := range m.mgServers {
			marker := "  "
			if i == m.mgIdx {
				marker = selStyle.Render("▸ ")
			}
			state := "stopped"
			if s.Active {
				state = "running"
			}
			sb.WriteString(fmt.Sprintf("%s%s  port %d  %-18s  %-8s  clients:%d  bundle:%v\n",
				marker, s.Name, s.Port, s.Subnet, state, len(s.Clients), s.Bundle))
		}
		if m.mgMsg != "" {
			sb.WriteString("\n" + m.mgMsg + "\n")
		}
		sb.WriteString(helpStyle.Render("\nPick a server with ↑/↓, then choose:\n"))
		sb.WriteString(helpStyle.Render("  1 restart  2 clients  3 logs  4 list-users  5 add-user  6 change-password  7 delete-user  8 revoke-cert  9 delete SERVER (asks name, backup kept)  • 0/esc back\n"))
		sb.WriteString(helpStyle.Render("  Single-key shortcuts still work: r c l u a p d v x — but numbers are easier to read\n"))
	case stagePrompt:
		sb.WriteString(m.prompt.title + "\n\n" + m.prompt.input.View() + "\n\n")
		sb.WriteString(helpStyle.Render("enter confirm • ") + back)
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
		if len(m.results) > 0 {
			allOK := true
			for _, r := range m.results {
				if !r.OK {
					allOK = false
				}
			}
			if allOK {
				sb.WriteString("\n" + selStyle.Render("All checks passed — server "+m.cfg.Host+":"+strconv.Itoa(m.cfg.Port)+" is ready."))
			} else {
				sb.WriteString("\n" + warnStyle.Render("Some checks failed — see ✘ above, or run wizard --check again."))
			}
		}
		dlHost := m.cfg.Fallback
		if dlHost == "" {
			dlHost = m.cfg.Host
		}
		ovpn := manage.BundleRoot + "/" + m.cfg.VPNUser + ".ovpn"
		sb.WriteString("\n\n" + warnStyle.Render("Get the file on your device:") + "\n")
		sb.WriteString(fmt.Sprintf("  scp root@%s:\"%s\" ./%s.ovpn\n", dlHost, ovpn, m.cfg.VPNUser))
		sb.WriteString(fmt.Sprintf("  If scp asks for a password: your VPS root password. With a key: scp -i ~/.ssh/id_rsa root@%s:\"%s\" ./%s.ovpn\n", dlHost, ovpn, m.cfg.VPNUser))
		sb.WriteString(fmt.Sprintf("  Then on the phone: OpenVPN app → Import %s.ovpn → ", m.cfg.VPNUser))
		if m.cfg.NoPassword {
			sb.WriteString("connect (cert-only, no password).\n")
		} else {
			sb.WriteString(fmt.Sprintf("user %s → password you set.\n", m.cfg.VPNUser))
		}
		sb.WriteString(warnStyle.Render("  Need help?  ") + "wizard --help  •  wizard --manage list  •  wizard --check\n")
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

func suggestPassword() string {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	b := make([]byte, 12)
	for i := range b {
		b[i] = letters[int(time.Now().UnixNano()+int64(i*7919))%len(letters)]
		// tiny jitter so two quick calls differ
		time.Sleep(time.Microsecond)
	}
	return string(b)
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
