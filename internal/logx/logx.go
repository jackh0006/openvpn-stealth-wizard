package logx

import (
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

var (
	okStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	failStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	infoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	dimStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
)

// Logger is a threadsafe line buffer. Steps append; the TUI renders.
type Logger struct {
	mu    sync.Mutex
	lines []string
	ch    chan struct{}
}

func New() *Logger { return &Logger{ch: make(chan struct{}, 1)} }

func (l *Logger) notify() {
	select {
	case l.ch <- struct{}{}:
	default:
	}
}

// Changed returns a channel pulsed on every append.
func (l *Logger) Changed() <-chan struct{} { return l.ch }

func (l *Logger) append(s string) {
	l.mu.Lock()
	l.lines = append(l.lines, s)
	if len(l.lines) > 2000 {
		l.lines = l.lines[len(l.lines)-2000:]
	}
	l.mu.Unlock()
	l.notify()
}

func (l *Logger) Info(format string, a ...any) {
	l.append(infoStyle.Render("  " + sprintf(format, a...)))
}
func (l *Logger) OK(msg string)   { l.append(okStyle.Render("  ✔ " + msg)) }
func (l *Logger) Fail(msg string) { l.append(failStyle.Render("  ✘ " + msg)) }
func (l *Logger) Dim(msg string)  { l.append(dimStyle.Render("  " + msg)) }
func (l *Logger) Raw(line string) { l.append("  " + line) }
func (l *Logger) StepTitle(n, total int, t string) {
	l.append("")
	l.append(lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true).
		Render(sprintf("━━━ [%d/%d] %s ━━━", n, total, t)))
}

// Lines returns a copy of all lines.
func (l *Logger) Lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// Tail returns the last n lines joined.
func (l *Logger) Tail(n int) string {
	all := l.Lines()
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return strings.Join(all, "\n")
}

func sprintf(f string, a ...any) string {
	if len(a) == 0 {
		return f
	}
	return fmt.Sprintf(f, a...)
}
