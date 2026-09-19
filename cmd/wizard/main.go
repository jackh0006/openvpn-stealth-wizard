package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jackh0006/openvpn-stealth-wizard/internal/cfg"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/logx"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/manage"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/steps"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/tui"
)

const version = "0.5.0"

func main() {
	checkOnly := flag.Bool("check", false, "read-only health check, changes nothing")
	doctor := flag.Bool("doctor", false, "smart diagnosis: read-only deep check with fix hints (like AI doctor)")
	fix := flag.Bool("fix", false, "re-apply missing pieces only (headless needs --yes)")
	nonInteractive := flag.Bool("non-interactive", false, "no TUI; use flags")
	yes := flag.Bool("yes", false, "apply without asking (with --non-interactive)")
	ver := flag.Bool("version", false, "print version")
	help := flag.Bool("help", false, "print usage with examples and exit")
	mode := flag.String("mode", "domain", "ip or domain")
	host := flag.String("host", "", "server IP or domain")
	fallback := flag.String("fallback", "", "fallback IP (DNS bypass)")
	port := flag.Int("port", 443, "vpn TCP port (stealth default 443, any 1-65535)")
	proto := flag.String("proto", "tcp", "tcp | udp | both (both = stealth TCP + fast UDP)")
	udpPort := flag.Int("udp-port", 1194, "vpn UDP port when --proto udp|both")
	user := flag.String("user", "", "vpn username")
	pass := flag.String("pass", "", "vpn password (empty + --gen-pass = suggest one; --no-password = cert-only; --no-auth = no login)")
	noPass := flag.Bool("no-password", false, "cert-only mode: no password, cert alone is enough")
	noAuth := flag.Bool("no-auth", false, "TESTING ONLY: no password and no cert-check (insecure! needs --yes-i-know-insecure)")
	knowInsecure := flag.Bool("yes-i-know-insecure", false, "confirm you understand --no-auth is insecure and for testing only")
	genPass := flag.Bool("gen-pass", false, "generate a strong password when --pass is empty")
	email := flag.String("email", "", "letsencrypt contact (domain mode)")
	dns := flag.String("dns", "cloudflare", "cloudflare|google|quad9|adguard|IP1[,IP2] (pushed DNS)")
	mtu := flag.Int("mtu", 1400, "tun-mtu (1200-1500, 1400 = LTE-safe)")
	mss := flag.Int("mss", 1200, "TCPMSS clamp (1000-1460, 1200 = carrier-proof)")
	freePort := flag.Bool("free-port", false, "stop the service owning --port (never SSH), then continue")
	mgAction := flag.String("manage", "", "manage action: list|restart|delete|user-list|user-add|user-pass|user-del|revoke|clients|logs|backup|uninstall|doctor")
	mgServer := flag.String("server", "server", "server name for --manage")
	mgUser := flag.String("muser", "", "username for user actions")
	mgPass := flag.String("mpass", "", "password for user-add/user-pass")
	uninstall := flag.Bool("uninstall", false, "uninstall everything this wizard created (backup kept, never touches SSH)")
	flag.Parse()

	if *ver {
		fmt.Println("openvpn-stealth-wizard " + version)
		return
	}

	if *help {
		fmt.Print(`openvpn-stealth-wizard v0.5.0: stealth VPN, guided setup. Like a 5-year-old guide + live logs.

START HERE (pick one):
  sudo wizard                  pretty step-by-step mode (recommended: proto, auth, domain/IP, DNS, live logs)
  wizard --check ...           look only, changes nothing (safe anywhere)
  sudo wizard --doctor ...     smart AI-like diagnosis with fix hints (read-only unless --fix --yes)

PROTOCOL (you asked: custom tcp/udp/both + any port):
  --proto tcp|udp|both  --port 443  --udp-port 1194
  tcp  = stealth (looks like a website on 443, port-share decoy) — best for blocked networks
  udp  = faster, less stealth — best for speed where VPN is allowed
  both = two servers at once (TCP 443 + UDP 1194), two .ovpn files, phone tries UDP then TCP

LOGIN MODES:
  default            username + password + certificate (most secure)
  --no-password      cert-only (phone file alone is enough, no password typed)
  --no-auth          TESTING ONLY, INSECURE (no login at all — needs --yes-i-know-insecure)

EXAMPLES:
  # health check (read-only)
  wizard --check --mode domain --host vpn.example.com \
    --user alice --pass 'secret' --email admin@example.com

  # smart doctor (read-only deep diagnosis)
  sudo wizard --doctor --mode ip --host 80.240.25.85 --user alice --pass 'x'

  # full install, no questions (TCP stealth 443)
  sudo wizard --non-interactive --yes \
    --mode domain --host vpn.example.com --fallback 203.0.113.10 \
    --proto tcp --port 443 --user alice --pass 'S3cure!!' --email admin@example.com

  # both protocols (stealth + speed)
  sudo wizard --non-interactive --yes \
    --mode ip --host 203.0.113.10 --proto both --port 443 --udp-port 1194 \
    --user alice --pass 'S3cure!!'

  # cert-only (no password)
  sudo wizard --non-interactive --yes --no-password \
    --mode domain --host vpn.example.com --user alice --email admin@example.com

  # testing-only no-auth (INSECURE!)
  sudo wizard --non-interactive --yes --no-auth --yes-i-know-insecure \
    --mode ip --host 203.0.113.10 --user alice

  # custom DNS + MTU/MSS tuning for carriers
  sudo wizard --non-interactive --yes --dns google --mtu 1400 --mss 1200 ... (install flags)

  # IP-only server (no domain, no email needed)
  sudo wizard --non-interactive --yes \
    --mode ip --host 203.0.113.10 --port 443 --user alice --pass 'S3cure!!'

  # fix what's missing, keep what's fine (THE traffic fixer)
  sudo wizard --non-interactive --yes --fix ... (same flags as install)

DOMAIN vs IP + CLOUDFLARE (both explained):
  IP mode:      just works, no DNS needed. Use --mode ip --host <VPS-IP>.
  Domain mode:  needs A record <host> -> <VPS-IP>.
    DNS-only (grey cloud, "DNS only"): VPN WORKS. Use this for vpn.example.com.
    Proxied (orange cloud, "Proxied"): VPN BREAKS (Cloudflare hides your VPS).
      Use orange cloud only for a separate website hostname, never for VPN.
    If phones can't resolve DNS (carrier blocks), --fallback <VPS-IP> saves you.

FLAGS:
  --check            health check only, exit 0 = healthy, 1 = sick
  --doctor           smart diagnosis + hints (read-only unless --fix --yes)
  --fix              re-apply missing pieces only
  --non-interactive  no pretty screens, use flags (needs --yes to change anything)
  --yes              I understand, change the system
  --mode ip|domain   --host NAME --fallback IP --port N --proto tcp|udp|both --udp-port N
  --user NAME --pass SECRET  (--gen-pass = generate, --no-password = cert-only, --no-auth = insecure test)
  --dns cloudflare|google|quad9|adguard|1.1.1.1,1.0.0.1  --mtu 1400 --mss 1200
  --email ADDR       --free-port to take a busy port (never SSH)  --manage list etc.
  --version          print version and exit

EXIT CODES: 0 ok, 1 something failed, 2 bad flags (nothing was touched).
`)
		return
	}

	if *uninstall || *mgAction == "uninstall" {
		fmt.Print("Type UNINSTALL to confirm removing everything (backup kept, SSH untouched): ")
		var c string
		fmt.Scanln(&c)
		if c != "UNINSTALL" {
			fmt.Fprintln(os.Stderr, "kept -- type UNINSTALL exactly to confirm")
			os.Exit(2)
		}
		if err := (steps.Uninstall{}).Apply(context.Background(), cfg.Defaults(), logx.New()); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		fmt.Println("uninstall done -- backup in /root/openvpn-*.tar.gz")
		os.Exit(0)
	}

	if *nonInteractive || *checkOnly || *doctor || *mgAction != "" || *freePort {
		if *mgAction != "" {
			follow := false
			if *mgAction == "logs" {
				for _, a := range os.Args {
					if a == "--follow" || a == "-f" {
						follow = true
					}
				}
			}
			if *mgAction == "doctor" {
				// Smart doctor without install flags: use defaults + live system state.
				c := cfg.Defaults()
				c.Normalize()
				fmt.Println("== wizard doctor (read-only, smart hints) ==")
				fmt.Println("TIP: for full checks pass --mode/--host/--user like install.")
				os.Exit(tui.CheckHeadless(os.Stdout, c))
			}
			os.Exit(runManage(*mgAction, *mgServer, *mgUser, *mgPass, follow))
		}
		c := cfg.Defaults()
		c.Mode = cfg.Mode(*mode)
		c.Host, c.Fallback, c.Port = *host, *fallback, *port
		c.Proto, c.UdpPort = *proto, *udpPort
		c.VPNUser, c.Email = *user, *email
		c.NoPassword = *noPass
		c.NoAuth = *noAuth
		c.Mtu, c.Mss = *mtu, *mss
		applyDNSChoice(&c, *dns)
		c.Normalize()
		if *noAuth && !*knowInsecure {
			fmt.Fprintln(os.Stderr, "bad flags: --no-auth is INSECURE testing-only; add --yes-i-know-insecure to confirm you understand anyone with the .ovpn can connect")
			os.Exit(2)
		}
		if *noAuth {
			fmt.Fprintln(os.Stderr, "WARNING: --no-auth testing mode (INSECURE): anyone with the .ovpn can connect. Use only for testing!")
			c.VPNPass = ""
		} else if *noPass {
			c.VPNPass = ""
		} else if *pass == "" && *genPass {
			c.VPNPass = genSuggestedPass()
			fmt.Println("generated password for", *user, ":", c.VPNPass, "(save it -- shown only once)")
		} else {
			c.VPNPass = *pass
		}
		if err := c.Validate(); err != nil {
			fmt.Fprintln(os.Stderr, "bad flags: "+err.Error())
			os.Exit(2)
		}
		if *doctor {
			fmt.Println("== wizard doctor (read-only) ==")
			fmt.Printf("target: %s %s:%d proto=%s user=%s dns=%s,%s mtu=%d mss=%d\n",
				c.Mode, c.Host, c.Port, c.EffectiveProto(), c.VPNUser, c.DNS1, c.DNS2, c.Mtu, c.Mss)
			code := tui.CheckHeadless(os.Stdout, c)
			fmt.Println("\nHints:")
			fmt.Println("  connects but no traffic? -> sudo wizard --non-interactive --yes --fix ... (same flags) fixes NAT/forwarding/MSS (the #1 cause).")
			fmt.Println("  Cloudflare: grey cloud (DNS-only) for VPN; orange cloud breaks VPN.")
			fmt.Println("  Android: import the -tcp file first (stealth), keep fallback IP remote.")
			os.Exit(code)
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

func applyDNSChoice(c *cfg.Config, raw string) {
	s := strings.TrimSpace(strings.ToLower(raw))
	switch s {
	case "", "cloudflare":
		c.DNS1, c.DNS2 = cfg.DNSPresets["cloudflare"][0], cfg.DNSPresets["cloudflare"][1]
	case "google":
		c.DNS1, c.DNS2 = cfg.DNSPresets["google"][0], cfg.DNSPresets["google"][1]
	case "quad9":
		c.DNS1, c.DNS2 = cfg.DNSPresets["quad9"][0], cfg.DNSPresets["quad9"][1]
	case "adguard":
		c.DNS1, c.DNS2 = cfg.DNSPresets["adguard"][0], cfg.DNSPresets["adguard"][1]
	default:
		// Accept "1.1.1.1,1.0.0.1" or "1.1.1.1 1.0.0.1" or single IP.
		clean := strings.ReplaceAll(raw, ",", " ")
		parts := strings.Fields(clean)
		if len(parts) >= 2 {
			c.DNS1, c.DNS2 = parts[0], parts[1]
		} else if len(parts) == 1 {
			c.DNS1 = parts[0]
		}
	}
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
