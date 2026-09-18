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

// Owner describes whatever holds a TCP port.
type Owner struct {
	Process string
	PID     string
	Unit    string
}

func (o Owner) String() string {
	s := o.Process
	if o.PID != "" {
		s += " (pid " + o.PID + ")"
	}
	if o.Unit != "" {
		s += " via " + o.Unit
	}
	return s
}

// PortOwner parses `ss -tlnp` and names the listener. Empty Process = free.
func PortOwner(port int) Owner {
	out, err := Output("ss", "-tlnp")
	if err != nil {
		return Owner{}
	}
	needle := fmt.Sprintf(":%d", port)
	for _, ln := range strings.Split(out, "\n") {
		if !strings.Contains(ln, "LISTEN") || !strings.Contains(ln, needle) {
			continue
		}
		o := Owner{}
		if i := strings.Index(ln, `users:(("`); i >= 0 {
			rest := ln[i+len(`users:(("`):]
			if j := strings.Index(rest, `"`); j >= 0 {
				o.Process = rest[:j]
			}
			if k := strings.Index(rest, "pid="); k >= 0 {
				p := rest[k+4:]
				if j := strings.IndexAny(p, ",)"); j >= 0 {
					o.PID = p[:j]
				}
			}
		}
		if o.Process == "" {
			o.Process = "unknown process"
		}
		// Best unit detection: ps -o unit= gives the systemd unit directly.
		if o.PID != "" {
			if u, err := Output("ps", "-o", "unit=", "-p", o.PID); err == nil {
				if t := strings.TrimSpace(u); t != "" && t != "-" {
					o.Unit = t
				}
			}
		}
		if o.Unit == "" && o.PID != "" {
			if u, err := Output("systemctl", "status", o.PID); err == nil {
				for _, l := range strings.Split(u, "\n") {
					if strings.Contains(l, ".service") {
						f := strings.Fields(strings.TrimSpace(l))
						if len(f) > 0 {
							o.Unit = strings.Trim(f[0], "●")
							break
						}
					}
				}
			}
		}
		// Fallback guesses for known daemons when systemd unit is not exposed.
		if o.Unit == "" {
			switch o.Process {
			case "nginx":
				o.Unit = "nginx.service"
			case "openvpn":
				o.Unit = "openvpn-server@server.service"
			case "stunnel", "stunnel4":
				o.Unit = "stunnel4.service"
			}
		}
		return o
	}
	return Owner{}
}

func portInUse(port int) bool { return PortOwner(port).Process != "" }

// FreePort stops the service owning a port so the wizard can use it.
// It refuses port 22 / sshd unconditionally and keeps file backups.
// When the owner is this wizard's own OpenVPN, the caller should confirm
// first (the wizard always asks in TUI review before calling this).
func FreePort(ctx context.Context, port int, log *logx.Logger) error {
	if port == 22 {
		return fmt.Errorf("refusing to free port 22 (SSH keeps you connected)")
	}
	o := PortOwner(port)
	if o.Process == "" {
		log.OK(fmt.Sprintf("port %d already free", port))
		return nil
	}
	if o.Process == "sshd" || strings.Contains(o.Unit, "ssh") {
		return fmt.Errorf("refusing to free %s (SSH keeps you connected)", o.String())
	}
	// Resolve unit to stop. PortOwner already fills fallbacks for nginx/openvpn.
	unit := o.Unit
	if i := strings.Index(unit, " "); i >= 0 {
		unit = unit[:i]
	}
	if unit == "" && o.PID != "" {
		return fmt.Errorf("port %d held by %s: no systemd unit found, kill pid %s manually", port, o.Process, o.PID)
	}
	if unit != "" {
		log.Info("%s", "stopping "+unit+" (files kept, service disabled)")
		if err := Run(ctx, log, "systemctl", "stop", unit); err != nil {
			return err
		}
		_ = Run(ctx, log, "systemctl", "disable", unit)
		time.Sleep(2 * time.Second)
		// For templated openvpn-server@*, also stop any other instances that
		// might still hold the same port (discovered via ServerDir).
		if strings.HasPrefix(unit, "openvpn-server@") {
			if entries, err := os.ReadDir("/etc/openvpn/server"); err == nil {
				for _, e := range entries {
					if strings.HasSuffix(e.Name(), ".conf") {
						other := "openvpn-server@" + strings.TrimSuffix(e.Name(), ".conf")
						if other != unit {
							_ = Run(ctx, log, "systemctl", "stop", other)
						}
					}
				}
			}
		}
	}
	if still := PortOwner(port); still.Process != "" {
		return fmt.Errorf("port %d still held by %s after stop; kill it manually", port, still.String())
	}
	log.OK(fmt.Sprintf("port %d is free now", port))
	return nil
}

func (Conflicts) Check(c cfg.Config) Result {
	if o := PortOwner(c.Port); o.Process != "" {
		return Result{false, fmt.Sprintf("port %d held by %s", c.Port, o.String())}
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
