package check

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jackh0006/openvpn-stealth-wizard/internal/cfg"
)

// Item is one health probe result.
type Item struct {
	Name   string
	OK     bool
	Detail string
}

func shellOut(timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	b, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil && len(b) == 0 {
		return "ERR: " + err.Error()
	}
	return strings.TrimSpace(string(b))
}

// All runs every probe. Read-only: never changes the system.
func All(c cfg.Config) []Item {
	var items []Item
	add := func(name string, ok bool, detail string) {
		items = append(items, Item{name, ok, detail})
	}

	svc := shellOut(10*time.Second, "systemctl", "is-active", "openvpn-server@server")
	add("VPN service running", svc == "active", svc)

	ss := shellOut(10*time.Second, "ss", "-tln")
	add(fmt.Sprintf("port %d listening", c.Port),
		strings.Contains(ss, fmt.Sprintf(":%d", c.Port)), firstMatch(ss, fmt.Sprintf(":%d", c.Port)))

	route := shellOut(10*time.Second, "ip", "route", "get", "10.8.0.2")
	add("return route into tunnel", strings.Contains(route, "tun0"), oneLine(route))

	nat := shellOut(10*time.Second, "iptables", "-t", "nat", "-L", "POSTROUTING", "-n")
	add("NAT masquerade", strings.Contains(nat, "10.8.0.0"), "POSTROUTING has VPN subnet")

	conf, err := os.ReadFile("/etc/openvpn/server/server.conf")
	if err != nil {
		add("server.conf sane", false, "unreadable")
	} else {
		s := string(conf)
		ok := strings.Contains(s, "auth-user-pass-verify") && strings.Contains(s, "via-file") &&
			strings.Contains(s, "port-share 127.0.0.1 8443")
		add("server.conf sane", ok, "password-via-file + port-share present")
	}

	if c.Mode == cfg.ModeDomain {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		ips, err := net.DefaultResolver.LookupHost(ctx, c.Host)
		cancel()
		detail := strings.Join(ips, ",")
		if err != nil {
			detail = err.Error()
		}
		add("domain resolves: "+c.Host, err == nil && len(ips) > 0, detail)
	}

	web := shellOut(15*time.Second, "curl", "-sk", "--max-time", "10",
		fmt.Sprintf("https://127.0.0.1:%d/", 8443))
	add("decoy website answers", strings.Contains(web, "Welcome"), trunc(web, 60))

	return items
}

func firstMatch(s, sub string) string {
	for _, ln := range strings.Split(s, "\n") {
		if strings.Contains(ln, "LISTEN") && strings.Contains(ln, sub) {
			return strings.Join(strings.Fields(ln), " ")
		}
	}
	return "not listening"
}

func oneLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func trunc(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
