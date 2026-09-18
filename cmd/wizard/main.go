package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jackh0006/openvpn-stealth-wizard/internal/cfg"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/tui"
)

const version = "0.1.0"

func main() {
	checkOnly := flag.Bool("check", false, "read-only health check, changes nothing")
	fix := flag.Bool("fix", false, "re-apply missing pieces only (headless needs --yes)")
	nonInteractive := flag.Bool("non-interactive", false, "no TUI; use flags")
	yes := flag.Bool("yes", false, "apply without asking (with --non-interactive)")
	ver := flag.Bool("version", false, "print version")
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
