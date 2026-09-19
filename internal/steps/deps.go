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

// binForPkg maps apt package -> binary to probe. ca-certificates has no
// binary; we check for the bundle file instead. iproute2 provides ip+ss.
var binForPkg = map[string]string{
	"openvpn":         "openvpn",
	"openssl":         "openssl",
	"iptables":        "iptables",
	"iproute2":        "ip",
	"nginx":           "nginx",
	"ufw":             "ufw",
	"curl":            "curl",
	"ca-certificates": "",
}

func (Dependencies) Check(c cfg.Config) Result {
	var missing []string
	for pkg, bin := range binForPkg {
		if pkg == "ca-certificates" {
			if !fileExists("/etc/ssl/certs/ca-certificates.crt") {
				// Also accept update-ca-certificates binary as present.
				if _, err := exec.LookPath("update-ca-certificates"); err != nil {
					missing = append(missing, pkg)
				}
			}
			continue
		}
		if bin == "" {
			continue
		}
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, pkg)
		}
	}
	// ss may live in iproute2 but some minimal images lack it; check too.
	if _, err := exec.LookPath("ss"); err != nil {
		if _, err2 := exec.LookPath("ip"); err2 != nil {
			// already counted via iproute2
		} else {
			// ip present but ss missing -> still need iproute2 full
			found := false
			for _, m := range missing {
				if m == "iproute2" {
					found = true
				}
			}
			if !found {
				missing = append(missing, "iproute2")
			}
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
	for pkg, bin := range binForPkg {
		if pkg == "ca-certificates" {
			if !fileExists("/etc/ssl/certs/ca-certificates.crt") {
				if _, err := exec.LookPath("update-ca-certificates"); err != nil {
					need = append(need, pkg)
				}
			}
			continue
		}
		if _, err := exec.LookPath(bin); err != nil {
			need = append(need, pkg)
		}
	}
	if len(need) == 0 {
		log.OK("dependencies already satisfied")
		return nil
	}
	log.Info("installing: %s", strings.Join(need, ", "))
	// Step 1: update package lists (no package args — old code passed
	// packages to `apt-get update` which is invalid).
	if err := Run(ctx, log, "apt-get", "update"); err != nil {
		log.Dim("apt-get update had warnings, continuing to install anyway: " + err.Error())
	}
	args := append([]string{"install", "-y"}, need...)
	if err := Run(ctx, log, "apt-get", args...); err != nil {
		return err
	}
	// If domain mode, certbot helps get Let's Encrypt for the decoy site.
	// Installed lazily so IP-only users don't pay the cost.
	if c.Mode == cfg.ModeDomain {
		if _, err := exec.LookPath("certbot"); err != nil {
			log.Info("domain mode: installing certbot for free HTTPS certificate (optional, decoy works without it)")
			_ = Run(ctx, log, "apt-get", "install", "-y", "certbot", "python3-certbot-nginx")
		}
	}
	log.OK("dependencies installed")
	return nil
}
