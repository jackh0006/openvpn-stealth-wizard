package steps

import (
	"strings"
	"testing"

	"github.com/jackh0006/openvpn-stealth-wizard/internal/cfg"
)

func testCfg() cfg.Config {
	c := cfg.Defaults()
	c.Host = "vpn.example.com"
	c.Fallback = "203.0.113.10"
	c.Port = 443
	c.VPNUser = "alice"
	c.VPNPass = "S3cure!!-testpass"
	c.Email = "admin@example.com"
	return c
}

func TestServerConfText(t *testing.T) {
	s := serverConfText(testCfg())
	for _, want := range []string{
		"port 443",
		"port-share 127.0.0.1 8443",
		"auth-user-pass-verify /etc/openvpn/check-pass.sh via-file",
		"route 10.8.0.0 255.255.255.0",
		"push \"dhcp-option DNS 1.1.1.1\"",
		"verify-client-cert require",
		"tls-crypt",
		`push "block-outside-dns"`,
		`push "redirect-gateway def1`,
		"tun-mtu 1400",
		"mssfix 1200",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("server.conf lacks %q", want)
		}
	}
	if strings.Contains(s, "via-env") {
		t.Error("must use via-file, never via-env (systemd loses env)")
	}
	if strings.Contains(s, `push "sndbuf`) {
		t.Error("must not push sndbuf (breaks Android buffers)")
	}
}

func TestServerConfCertOnly(t *testing.T) {
	c := testCfg()
	c.NoPassword = true
	s := serverConfText(c)
	if strings.Contains(s, "auth-user-pass-verify") {
		t.Error("cert-only must not contain password auth")
	}
	if !strings.Contains(s, "verify-client-cert require") {
		t.Error("cert-only must still require cert")
	}
}

func TestServerConfNoAuth(t *testing.T) {
	c := testCfg()
	c.NoAuth = true
	s := serverConfTextForProto(c, "tcp")
	if strings.Contains(s, "auth-user-pass-verify") {
		t.Error("no-auth must not contain password auth")
	}
	if !strings.Contains(s, "client-cert-not-required") {
		t.Error("no-auth must contain client-cert-not-required")
	}
}

func TestServerConfUDP(t *testing.T) {
	c := testCfg()
	c.Proto = "udp"
	c.UdpPort = 1194
	s := serverConfTextForProto(c, "udp")
	if !strings.Contains(s, "proto udp") {
		t.Error("udp conf must contain proto udp")
	}
	if !strings.Contains(s, "port 1194") {
		t.Error("udp conf must contain port 1194")
	}
	if strings.Contains(s, "port-share") && !strings.Contains(s, "# no port-share") {
		t.Error("udp must not have active port-share")
	}
}

func TestServerConfBothPorts(t *testing.T) {
	c := testCfg()
	c.Proto = "both"
	c.UdpPort = 1194
	tcp := serverConfTextForProto(c, "tcp")
	udp := serverConfTextForProto(c, "udp")
	if !strings.Contains(tcp, "port 443") || !strings.Contains(udp, "port 1194") {
		t.Error("both mode must produce distinct TCP+UDP ports")
	}
}

func TestStepOrder(t *testing.T) {
	got := []string{}
	for _, s := range All() {
		got = append(got, s.ID())
	}
	want := []string{"deps", "preflight", "conflicts", "pki", "server", "web", "auth", "net", "client"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("runbook order changed: %v", got)
	}
}
