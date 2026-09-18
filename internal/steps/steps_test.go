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
	} {
		if !strings.Contains(s, want) {
			t.Errorf("server.conf lacks %q", want)
		}
	}
	if strings.Contains(s, "via-env") {
		t.Error("must use via-file, never via-env (systemd loses env)")
	}
}

func TestStepOrder(t *testing.T) {
	got := []string{}
	for _, s := range All() {
		got = append(got, s.ID())
	}
	want := []string{"preflight", "conflicts", "pki", "server", "web", "auth", "net", "client"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("runbook order changed: %v", got)
	}
}
