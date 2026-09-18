package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jackh0006/openvpn-stealth-wizard/internal/cfg"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/logx"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/manage"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/steps"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/tui"
)

const version = "0.3.0"

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
	pass := flag.String("pass", "", "vpn password (empty + --gen-pass = suggest one; --no-password = cert-only)")
	noPass := flag.Bool("no-password", false, "cert-only mode: no password, cert alone is enough")
	genPass := flag.Bool("gen-pass", false, "generate a strong password when --pass is empty")
	email := flag.String("email", "", "letsencrypt contact (domain mode)")
	freePort := flag.Bool("free-port", false, "stop the service owning --port (never SSH), then continue")
	mgAction := flag.String("manage", "", "manage action: list|restart|delete|user-list|user-add|user-pass|user-del|revoke|clients|logs|backup")
	mgServer := flag.String("server", "server", "server name for --manage")
	mgUser := flag.String("muser", "", "username for user actions")
	mgPass := flag.String("mpass", "", "password for user-add/user-pass")
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

  # cert-only (no password, generate or skip --pass)
  sudo wizard --non-interactive --yes --no-password \
    --mode domain --host vpn.example.com --user alice --email admin@example.com

  # auto-generate password if you leave --pass empty
  sudo wizard --non-interactive --yes --gen-pass \
    --mode domain --host vpn.example.com --user alice --email admin@example.com

  # IP-only server (no domain, no email needed)
  sudo wizard --non-interactive --yes \
    --mode ip --host 203.0.113.10 --port 443 --user alice --pass 'S3cure!!'

  # fix what's missing, keep what's fine
  sudo wizard --non-interactive --yes --fix ... (same flags as install)

  # free a taken port first (never touches SSH)
  sudo wizard --non-interactive --yes --free-port --port 443 ... (install flags)

  # manage existing servers (no install flags needed)
  sudo wizard --manage list
  sudo wizard --manage user-add --muser alice --mpass 'S3cure!!'
  sudo wizard --manage user-pass --muser alice --mpass 'N3w-pass!!'
  sudo wizard --manage user-del --muser alice
  sudo wizard --manage restart --server server
  sudo wizard --manage delete --server server   (asks for the name to confirm)
  sudo wizard --manage revoke --muser oldphone
  sudo wizard --manage clients --server server
  sudo wizard --manage logs
  sudo wizard --manage backup

FLAGS:
  --check            health check only, exit 0 = healthy, 1 = sick
  --fix              re-apply missing pieces only
  --non-interactive  no pretty screens, use flags (needs --yes to change anything)
  --yes              I understand, change the system
  --mode ip|domain   --host NAME --fallback IP --port N (default 443)
  --user NAME --pass SECRET  (--gen-pass = generate when empty, --no-password = cert-only)
  --email ADDR       --free-port to take a busy port (never SSH)  --manage list etc.
  --version          print version and exit

EXIT CODES: 0 ok, 1 something failed, 2 bad flags (nothing was touched).
`)
		return
	}

	if *nonInteractive || *checkOnly || *mgAction != "" || *freePort {
		if *mgAction != "" {
			follow := false
			if *mgAction == "logs" {
				for _, a := range os.Args {
					if a == "--follow" || a == "-f" {
						follow = true
					}
				}
			}
			os.Exit(runManage(*mgAction, *mgServer, *mgUser, *mgPass, follow))
		}
		c := cfg.Defaults()
		c.Mode = cfg.Mode(*mode)
		c.Host, c.Fallback, c.Port = *host, *fallback, *port
		c.VPNUser, c.Email = *user, *email
		c.NoPassword = *noPass
		if *noPass {
			c.VPNPass = ""
		} else if *pass == "" && *genPass {
			c.VPNPass = genSuggestedPass()
			fmt.Println("generated password for", *user, ":", c.VPNPass, "(save it — shown only once)")
		} else {
			c.VPNPass = *pass
		}
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
		if *freePort {
			log := logx.New()
			fmt.Fprintf(os.Stdout, "freeing port %d if taken (never SSH)…\n", c.Port)
			if err := steps.FreePort(context.Background(), c.Port, log); err != nil {
				fmt.Fprintln(os.Stderr, "cannot free port: "+err.Error())
				os.Exit(1)
			}
		}
		os.Exit(tui.RunHeadless(os.Stdout, c, *fix))
	}

	m := tui.New()
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui error: "+err.Error())
		os.Exit(1)
	}
}

// runManage executes one management action headlessly.
func runManage(action, server, user, pass string, follow bool) int {
	switch action {
	case "list":
		ss := manage.Discover()
		if len(ss) == 0 {
			fmt.Println("no servers under /etc/openvpn/server")
			return 1
		}
		for _, s := range ss {
			state := "stopped"
			if s.Active {
				state = "running"
			}
			fmt.Printf("%-12s port %-5d %-22s %-8s clients:%d bundle:%v\n",
				s.Name, s.Port, s.Subnet, state, len(s.Clients), s.Bundle)
		}
		return 0
	case "restart":
		if err := manage.Restart(server); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		fmt.Println(server + " restarted")
		return 0
	case "delete":
		fmt.Printf("type the server name (%s) to confirm deletion: ", server)
		var confirm string
		fmt.Scanln(&confirm)
		if confirm != server {
			fmt.Fprintln(os.Stderr, "name did not match, server kept")
			return 2
		}
		bak, err := manage.Delete(server)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		fmt.Println(server + " purged. Backup: " + bak)
		return 0
	case "user-list":
		for _, u := range manage.ListUsers() {
			fmt.Println(u)
		}
		return 0
	case "user-add", "user-pass":
		if err := manage.SetUser(user, pass); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		fmt.Println("user " + user + " ready")
		return 0
	case "user-del":
		if err := manage.DelUser(user); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		fmt.Println("user " + user + " deleted")
		return 0
	case "revoke":
		out, err := manage.RevokeClient(user)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		fmt.Println(user + ": " + out)
		return 0
	case "clients":
		cls := manage.ConnectedClients(server)
		if len(cls) == 0 {
			fmt.Println(server + ": no clients connected")
			return 1
		}
		for _, c := range cls {
			fmt.Println(c)
		}
		return 0
	case "logs":
		if follow {
			fmt.Println(manage.TailLog(30))
			fmt.Println("--- following (ctrl+c to stop) ---")
			last := ""
			for {
				time.Sleep(1500 * time.Millisecond)
				cur := manage.TailLog(30)
				if cur != last {
					fmt.Println(cur)
					last = cur
				}
			}
		}
		fmt.Println(manage.TailLog(30))
		return 0
	case "backup":
		bak, err := manage.BackupAll()
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		fmt.Println("backup: " + bak)
		return 0
	}
	fmt.Fprintln(os.Stderr, "unknown --manage action: "+action)
	return 2
}

func genSuggestedPass() string {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	b := make([]byte, 12)
	for i := range b {
		b[i] = letters[int(time.Now().UnixNano()+int64(i*7919))%len(letters)]
		time.Sleep(time.Microsecond)
	}
	return string(b)
}
