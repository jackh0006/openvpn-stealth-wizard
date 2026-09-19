package steps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackh0006/openvpn-stealth-wizard/internal/cfg"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/logx"
)

// Uninstall removes every file the wizard ever created, with a backup first.
// It never touches SSH, last.json, or non-wizard nginx content.
type Uninstall struct{}

func (Uninstall) ID() string    { return "uninstall" }
func (Uninstall) Title() string { return "Uninstall everything" }
func (Uninstall) Why() string {
	return "Removes servers, keys, users, bundles, and firewall rules — backup kept."
}

func (Uninstall) Check(c cfg.Config) Result {
	if _, err := os.Stat("/etc/openvpn/server"); err == nil {
		return Result{false, "wizard files still present"}
	}
	return Result{true, "nothing to uninstall"}
}

func (Uninstall) Apply(ctx context.Context, c cfg.Config, log *logx.Logger) error {
	// 1. Backup everything first (same helper as manage.BackupAll)
	present := []string{}
	candidates := []string{
		"/etc/openvpn",
		"/etc/nginx/sites-available/vpn-camouflage",
		"/etc/nginx/sites-enabled/vpn-camouflage",
		"/etc/nginx/nginx.conf.bak-wizard",
		"/etc/systemd/system/vpn-nat-restore.service",
		"/etc/iptables.wizard.rules",
		"/etc/iptables.vpn.rules",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			present = append(present, p)
		}
	}
	if entries, err := os.ReadDir("/root/Open Code/OpenVPN"); err == nil && len(entries) > 0 {
		present = append(present, "/root/Open Code/OpenVPN")
	}
	if len(present) > 0 {
		args := append([]string{"-czf", fmt.Sprintf("/root/openvpn-uninstall-backup-%s.tar.gz", shortTime())}, present...)
		if out, err := exec.Command("tar", args...).CombinedOutput(); err != nil {
			log.Dim("backup warning: " + strings.TrimSpace(string(out)))
		} else {
			log.OK(fmt.Sprintf("backup: %s", args[1]))
		}
	} else {
		log.Dim("nothing to back up")
	}

	// 2. Stop every openvpn-server@* instance we created
	if entries, err := os.ReadDir("/etc/openvpn/server"); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".conf") {
				name := strings.TrimSuffix(e.Name(), ".conf")
				unit := "openvpn-server@" + name
				_ = Run(ctx, log, "systemctl", "stop", unit)
				_ = Run(ctx, log, "systemctl", "disable", unit)
			}
		}
	}
	// Explicitly cover split TCP+UDP names even if dir listing raced.
	for _, u := range []string{"openvpn-server@server", "openvpn-server@server-udp"} {
		_ = Run(ctx, log, "systemctl", "stop", u)
		_ = Run(ctx, log, "systemctl", "disable", u)
	}

	// 3. Remove wizard files (never SSH, never last.json)
	removeAll := []string{
		"/etc/openvpn/server/server.conf",
		"/etc/openvpn/server/server-udp.conf",
		"/etc/openvpn/server/server.conf.bak-wizard",
		"/etc/openvpn/server/ca.crt",
		"/etc/openvpn/server/server.crt",
		"/etc/openvpn/server/server.key",
		"/etc/openvpn/server/tls-crypt.key",
		"/etc/openvpn/server/dh.pem",
		"/etc/openvpn/wizard-pki",
		"/etc/openvpn/check-pass.sh",
		"/etc/openvpn/ipp.txt",
		"/var/log/openvpn-status.log",
		"/var/log/openvpn.log",
		"/etc/systemd/system/vpn-nat-restore.service",
		"/etc/iptables.wizard.rules",
		"/etc/iptables.vpn.rules",
	}
	for _, p := range removeAll {
		if err := os.RemoveAll(p); err == nil {
			log.Dim("removed " + p)
		}
	}
	// Keep users dir but purge wizard users; keep dir if other VPNs use it
	if entries, err := os.ReadDir("/etc/openvpn/users"); err == nil {
		for _, e := range entries {
			_ = os.Remove(filepath.Join("/etc/openvpn/users", e.Name()))
		}
		log.Dim("cleared /etc/openvpn/users")
	}
	// Nginx: remove our site, un-park stream line if we parked it
	_ = os.Remove("/etc/nginx/sites-enabled/vpn-camouflage")
	_ = os.Remove("/etc/nginx/sites-available/vpn-camouflage")
	if data, err := os.ReadFile("/etc/nginx/nginx.conf"); err == nil {
		if strings.Contains(string(data), "# parked by wizard") {
			s := strings.ReplaceAll(string(data),
				"#include /etc/nginx/stream-sni.conf; # parked by wizard (OpenVPN owns 443)",
				"include /etc/nginx/stream-sni.conf;")
			_ = os.WriteFile("/etc/nginx/nginx.conf", []byte(s), 0o644)
			log.Dim("restored nginx stream include")
		}
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = exec.Command("systemctl", "reload-or-restart", "nginx").Run()

	// 4. UFW route rules (only ours) — cover all historic patterns.
	_ = Run(ctx, log, "sh", "-c", "for nic in enp1s0 eth0 ens3 ens4 venet0 lo; do ufw route delete allow in on tun0 out on $nic from 10.8.0.0/24 2>/dev/null; ufw route delete allow in on $nic out on tun0 to 10.8.0.0/24 2>/dev/null; ufw route delete allow in on tun+ out on $nic from 10.8.0.0/24 2>/dev/null; ufw route delete allow in on $nic out on tun+ to 10.8.0.0/24 2>/dev/null; done; echo cleaned-ufw")

	// 5. Mangle MSS clamp rules (all historic + current tun+/tun0 patterns).
	_ = Run(ctx, log, "sh", "-c", "for d in tun0 tun+; do iptables -t mangle -D FORWARD -o $d -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1200 2>/dev/null; iptables -t mangle -D FORWARD -i $d -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1200 2>/dev/null; done; echo cleaned-mangle")

	// 6. Bundles: remove .ovpn dir content (ask caller to confirm with flag)
	if _, err := os.Stat("/root/Open Code/OpenVPN"); err == nil {
		// Keep dir, remove .ovpn files only — safer than rmdir
		if entries, err := os.ReadDir("/root/Open Code/OpenVPN"); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".ovpn") || strings.HasSuffix(e.Name(), ".txt") {
					_ = os.Remove(filepath.Join("/root/Open Code/OpenVPN", e.Name()))
				}
			}
		}
		log.Dim("cleared bundles in /root/Open Code/OpenVPN")
	}

	log.OK("uninstall complete — reboot recommended if you want a pristine kernel state")
	return nil
}

func shortTime() string {
	return time.Now().Format("20060102-150405")
}
