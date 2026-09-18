package steps

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jackh0006/openvpn-stealth-wizard/internal/cfg"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/logx"
)

// Result is a read-only check outcome.
type Result struct {
	OK     bool
	Detail string
}

// Step is one wizard stage. Check never changes the system;
// Apply performs the change with backups.
type Step interface {
	ID() string
	Title() string
	Why() string
	Check(c cfg.Config) Result
	Apply(ctx context.Context, c cfg.Config, log *logx.Logger) error
}

// All returns install steps in runbook order.
func All() []Step {
	return []Step{
		Preflight{},
		Conflicts{},
		PKI{},
		ServerConf{},
		WebCamouflage{},
		PasswordAuth{},
		Network{},
		ClientBundle{},
	}
}

// Run executes one shell command, streaming each output line to the log.
func Run(ctx context.Context, log *logx.Logger, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		log.Raw(strings.TrimRight(sc.Text(), "\r"))
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// Output runs a command and returns trimmed stdout.
func Output(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(b)), err
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func backup(p string, log *logx.Logger) {
	if !fileExists(p) {
		return
	}
	dst := p + ".bak-wizard"
	in, err := os.ReadFile(p)
	if err != nil {
		return
	}
	if err := os.WriteFile(dst, in, 0o600); err == nil {
		log.Dim("backup: " + dst)
	}
}

// Preflight verifies OS, privileges and required binaries.
type Preflight struct{}

func (Preflight) ID() string    { return "preflight" }
func (Preflight) Title() string { return "Preflight checks" }
func (Preflight) Why() string {
	return "We look before we leap: right OS, root user, and all tools present."
}

func (Preflight) Check(c cfg.Config) Result {
	if os.Geteuid() != 0 {
		return Result{false, "not running as root (need root for VPN setup)"}
	}
	for _, b := range []string{"openvpn", "openssl", "iptables", "nginx", "systemctl"} {
		if _, err := exec.LookPath(b); err != nil {
			return Result{false, "missing binary: " + b}
		}
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil || !strings.Contains(string(data), "Ubuntu") {
		return Result{false, "/etc/os-release is not Ubuntu"}
	}
	return Result{true, "root on Ubuntu, all tools present"}
}

func (Preflight) Apply(ctx context.Context, c cfg.Config, log *logx.Logger) error {
	out, _ := Output("lsb_release", "-ds")
	log.OK("host: " + strings.Trim(out, `"`))
	out, _ = Output("openvpn", "--version")
	log.OK("openvpn: " + strings.SplitN(out, "\n", 2)[0])
	return nil
}

// Conflicts finds processes fighting over the VPN port (nginx stream
// vs stunnel vs openvpn) and stale --daemon test clients.
type Conflicts struct{}

func (Conflicts) ID() string    { return "conflicts" }
func (Conflicts) Title() string { return "Detect port conflicts" }
func (Conflicts) Why() string {
	return "Only one program can own port 443. We find squatters first."
}

func portInUse(port int) bool {
	out, err := Output("ss", "-tln")
	if err != nil {
		return false
	}
	needle := fmt.Sprintf(":%d", port)
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "LISTEN") && strings.Contains(ln, needle) {
			return true
		}
	}
	return false
}

func (Conflicts) Check(c cfg.Config) Result {
	if portInUse(c.Port) {
		return Result{false, fmt.Sprintf("port %d is already bound (see check output)", c.Port)}
	}
	return Result{true, fmt.Sprintf("port %d is free", c.Port)}
}

func (Conflicts) Apply(ctx context.Context, c cfg.Config, log *logx.Logger) error {
	if data, err := os.ReadFile("/etc/nginx/nginx.conf"); err == nil {
		if strings.Contains(string(data), "stream-sni.conf;") &&
			!strings.Contains(string(data), "#include /etc/nginx/stream-sni.conf") {
			log.Info("nginx stream SNI owns 443; it will be parked (backup kept)")
		}
	}
	if _, err := exec.LookPath("stunnel4"); err == nil {
		if out, _ := Output("systemctl", "is-active", "stunnel4"); out == "active" {
			log.Info("stunnel4 is active and may fight for 443; it will be stopped")
		}
	}
	log.OK("conflict scan done")
	return nil
}
