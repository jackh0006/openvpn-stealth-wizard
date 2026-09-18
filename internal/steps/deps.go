package steps

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/jackh0006/openvpn-stealth-wizard/internal/cfg"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/logx"
)

// Dependencies ensures every binary the wizard needs is present.
type Dependencies struct{}

func (Dependencies) ID() string    { return "deps" }
func (Dependencies) Title() string { return "Install dependencies" }
func (Dependencies) Why() string {
	return "We need the right tools (VPN, firewall, web server) before building anything."
}

var wantPkgs = []string{
	"openvpn", "openssl", "iptables", "iproute2", "nginx", "ufw", "curl", "ca-certificates",
}

var pkgForBin = map[string]string{
	"openvpn":         "openvpn",
	"openssl":         "openssl",
	"iptables":        "iptables",
	"nginx":           "nginx",
	"ufw":             "ufw",
	"curl":            "curl",
	"ca-certificates": "ca-certificates",
}

func (Dependencies) Check(c cfg.Config) Result {
	var missing []string
	for bin, pkg := range pkgForBin {
		if _, err := exec.LookPath(bin); err != nil {
			// iproute2 provides `ip`/`ss`, not a bin named iproute2
			if bin == "iproute2" {
				if _, err := exec.LookPath("ip"); err == nil {
					continue
				}
			}
			missing = append(missing, pkg)
		}
	}
	if len(missing) == 0 {
		return Result{true, "all dependencies present"}
	}
	return Result{false, "missing: " + strings.Join(missing, ", ")}
}

func (Dependencies) Apply(ctx context.Context, c cfg.Config, log *logx.Logger) error {
	if os.Geteuid() != 0 {
		return nil // cannot apt without root; Preflight will explain
	}
	if _, err := os.ReadFile("/etc/os-release"); err != nil {
		return nil
	}
	if _, err := exec.LookPath("apt-get"); err != nil {
		log.Dim("apt-get not found, skipping auto-install")
		return nil
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil || !strings.Contains(string(data), "Ubuntu") && !strings.Contains(string(data), "Debian") {
		log.Dim("not Debian/Ubuntu, skipping apt")
		return nil
	}
	var need []string
	for bin, pkg := range pkgForBin {
		if _, err := exec.LookPath(bin); err != nil {
			if bin == "iproute2" {
				if _, err := exec.LookPath("ip"); err == nil {
					continue
				}
				need = append(need, "iproute2")
				continue
			}
			need = append(need, pkg)
		}
	}
	if len(need) == 0 {
		log.OK("dependencies already satisfied")
		return nil
	}
	log.Info("installing: %s", strings.Join(need, ", "))
	if err := Run(ctx, log, "apt-get", append([]string{"update"}, need...)...); err != nil {
		// apt-get update is separate; try install even if update had warnings
		_ = err
	}
	args := append([]string{"install", "-y"}, need...)
	if err := Run(ctx, log, "apt-get", args...); err != nil {
		return err
	}
	log.OK("dependencies installed")
	return nil
}
