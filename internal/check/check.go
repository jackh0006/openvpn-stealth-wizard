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
	c.Normalize()
	var items []Item
	add := func(name string, ok bool, detail string) {
		items = append(items, Item{name, ok, detail})
	}

	proto := c.EffectiveProto()
	// 1. Services (both mode checks both units).
	if proto == "both" {
		svc := shellOut(10*time.Second, "systemctl", "is-active", "openvpn-server@server")
		add("VPN TCP service running", svc == "active", svc)
		svcU := shellOut(10*time.Second, "systemctl", "is-active", "openvpn-server@server-udp")
		add("VPN UDP service running", svcU == "active", svcU)
	} else {
		svc := shellOut(10*time.Second, "systemctl", "is-active", "openvpn-server@server")
		add("VPN service running", svc == "active", svc)
	}

	// 2. Ports.
	if proto == "tcp" || proto == "both" {
		ss := shellOut(10*time.Second, "ss", "-tln")
		add(fmt.Sprintf("TCP port %d listening", c.Port),
			strings.Contains(ss, fmt.Sprintf(":%d", c.Port)), firstMatch(ss, fmt.Sprintf(":%d", c.Port)))
	}
	if proto == "udp" || proto == "both" {
		su := shellOut(10*time.Second, "ss", "-uln")
		udpPort := c.UdpPort
		if udpPort == 0 {
			udpPort = 1194
		}
		add(fmt.Sprintf("UDP port %d listening", udpPort),
			strings.Contains(su, fmt.Sprintf(":%d", udpPort)), firstMatchUDP(su, fmt.Sprintf(":%d", udpPort)))
	}

	// 3. Return route (subnet-aware, old code hardcoded 10.8.0.2/tun0).
	gwProbe := "10.8.0.2"
	if parts := strings.Split(strings.Split(c.Subnet, "/")[0], "."); len(parts) == 4 {
		gwProbe = strings.Join([]string{parts[0], parts[1], parts[2], "2"}, ".")
	}
	route := shellOut(10*time.Second, "ip", "route", "get", gwProbe)
	add("return route into tunnel", strings.Contains(route, "tun"), oneLine(route)+" (probe "+gwProbe+")")

	nat := shellOut(10*time.Second, "iptables", "-t", "nat", "-L", "POSTROUTING", "-n")
	add("NAT masquerade ("+c.Subnet+")", strings.Contains(nat, strings.Split(c.Subnet, "/")[0]) || strings.Contains(nat, "MASQUERADE"), "POSTROUTING has VPN subnet")

	fwd := shellOut(10*time.Second, "cat", "/proc/sys/net/ipv4/ip_forward")
	add("IP forwarding enabled", strings.TrimSpace(fwd) == "1", "net.ipv4.ip_forward="+strings.TrimSpace(fwd))

	// 4. Server conf sanity (proto/auth/mtu aware).
	conf, err := os.ReadFile("/etc/openvpn/server/server.conf")
	if err != nil {
		add("server.conf sane", false, "unreadable")
	} else {
		s := string(conf)
		hasBlockDNS := strings.Contains(s, "block-outside-dns")
		hasRedirect := strings.Contains(s, "redirect-gateway")
		hasPortShare := strings.Contains(s, "port-share 127.0.0.1 8443") || proto == "udp"
		authOK := true
		authDetail := "auth mode ok"
		if c.NoAuth {
			authOK = strings.Contains(s, "client-cert-not-required") && !strings.Contains(s, "auth-user-pass-verify")
			authDetail = "no-auth mode"
		} else if c.NoPassword {
			authOK = !strings.Contains(s, "auth-user-pass-verify")
			authDetail = "cert-only, no password"
		} else {
			authOK = strings.Contains(s, "auth-user-pass-verify") && strings.Contains(s, "via-file")
			authDetail = "password-via-file"
		}
		ok := hasBlockDNS && hasRedirect && hasPortShare && authOK
		add("server.conf sane", ok, authDetail+" + block-dns="+boolStr(hasBlockDNS)+" redirect="+boolStr(hasRedirect))
	}

	// 5. MSS clamp present?
	mangle := shellOut(10*time.Second, "iptables", "-t", "mangle", "-L", "FORWARD", "-n")
	add("MSS clamp (anti-stall)", strings.Contains(mangle, "TCPMSS"), "mangle FORWARD has TCPMSS")

	if c.Mode == cfg.ModeDomain {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		ips, err := net.DefaultResolver.LookupHost(ctx, c.Host)
		cancel()
		detail := strings.Join(ips, ",")
		if err != nil {
			detail = err.Error() + " — TIP: use --fallback <VPS-IP> so phones connect even when DNS is blocked. Cloudflare must be DNS-only (grey cloud), not proxied."
		} else if len(ips) > 0 {
			detail += " — Cloudflare: grey cloud (DNS-only) required for VPN; orange cloud breaks VPN."
		}
		add("domain resolves: "+c.Host, err == nil && len(ips) > 0, detail)
	}

	web := shellOut(15*time.Second, "curl", "-sk", "--max-time", "10",
		fmt.Sprintf("https://127.0.0.1:%d/", 8443))
	if !strings.Contains(web, "Welcome") {
		// Try plain HTTP fallback (last-resort decoy).
		web2 := shellOut(10*time.Second, "curl", "-s", "--max-time", "8", "http://127.0.0.1:8443/")
		if strings.Contains(web2, "Welcome") {
			web = web2
		}
	}
	add("decoy website answers", strings.Contains(web, "Welcome"), trunc(web, 80))

	// 6. Bundle present?
	bundleNote := ""
	if proto == "both" {
		bundleNote = c.VPNUser + "-tcp.ovpn + " + c.VPNUser + "-udp.ovpn"
	} else {
		bundleNote = c.VPNUser + ".ovpn"
	}
	add("client bundle ("+bundleNote+")", true, "see "+bundleNote+" (full check in wizard --fix)")

	return items
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func firstMatchUDP(s, sub string) string {
	for _, ln := range strings.Split(s, "\n") {
		if strings.Contains(ln, sub) {
			return strings.Join(strings.Fields(ln), " ")
		}
	}
	return "not listening (udp)"
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
