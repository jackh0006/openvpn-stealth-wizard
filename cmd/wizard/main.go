package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jackh0006/openvpn-stealth-wizard/internal/cfg"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/tui"
)

const version = "0.1.2"

func main() {
	checkOnly := flag.Bool("check", false, "read-only health check, changes nothing")
	fix := flag.Bool("fix", false, "re-apply missing pieces only (headless needs --yes)")
	nonInteractive := flag.Bool("non-interactive", false, "no TUI; use flags")
	yes := flag.Bool("yes", false, "apply without asking (with --non-interactive)")
	ver := flag.Bool("version", false, "print version")
	help := flag.Bool("help", false, "print usage with examples and exit")
	mode := flag.String("mode", "domain", "ip or domain")
	host := flag.String("host", "", "server IP or domain")
	fallback := flag.String("fallback", "", "fallback IP (DNS bypass)")
	port := flag.Int("port", 443, "vpn port")
	user := flag.String("user", "", "vpn username")
	pass := flag.String("pass", "", "vpn password")
	email := flag.String("email", "", "letsencrypt contact (domain mode)")
	flag.Parse()

	if *ver {
		fmt.Println("openvpn-stealth-wizard " + version)
		return
	}

	if *help {
		fmt.Print(`openvpn-stealth-wizard: stealth VPN on port 443, guided setup.

START HERE (pick one):
  sudo wizard                  pretty step-by-step mode (recommended)
  wizard --check ...           look only, changes nothing (safe anywhere)

EXAMPLES:
  # health check (read-only)
  wizard --check --mode domain --host vpn.example.com \
    --user alice --pass 'secret' --email admin@example.com

  # full install, no questions
  sudo wizard --non-interactive --yes \
    --mode domain --host vpn.example.com --fallback 203.0.113.10 \
    --port 443 --user alice --pass 'S3cure!!' --email admin@example.com

  # IP-only server (no domain, no email needed)
  sudo wizard --non-interactive --yes \
    --mode ip --host 203.0.113.10 --port 443 --user alice --pass 'S3cure!!'

  # fix what's missing, keep what's fine
  sudo wizard --non-interactive --yes --fix ... (same flags as install)

FLAGS:
  --check            health check only, exit 0 = healthy, 1 = sick
  --fix              re-apply missing pieces only
  --non-interactive  no pretty screens, use flags (needs --yes to change anything)
  --yes              I understand, change the system
  --mode ip|domain   --host NAME --fallback IP --port N (default 443)
  --user NAME --pass SECRET --email ADDR
  --version          print version and exit

EXIT CODES: 0 ok, 1 something failed, 2 bad flags (nothing was touched).
`)
		return
	}

	if *nonInteractive || *checkOnly {
		c := cfg.Defaults()
		c.Mode = cfg.Mode(*mode)
		c.Host, c.Fallback, c.Port = *host, *fallback, *port
		c.VPNUser, c.VPNPass, c.Email = *user, *pass, *email
		if err := c.Validate(); err != nil {
			fmt.Fprintln(os.Stderr, "bad flags: "+err.Error())
			os.Exit(2)
		}
		if *checkOnly && !*fix {
			os.Exit(tui.CheckHeadless(os.Stdout, c))
		}
		if !*yes {
			fmt.Fprintln(os.Stderr, "refusing to change the system without --yes")
			os.Exit(2)
		}
		os.Exit(tui.RunHeadless(os.Stdout, c, *fix))
	}

	m := tui.New()
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui error: "+err.Error())
		os.Exit(1)
	}
}
