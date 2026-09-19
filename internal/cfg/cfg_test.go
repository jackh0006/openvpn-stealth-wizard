package cfg

import (
	"strings"
	"testing"
)

func TestValidateDomain(t *testing.T) {
	c := Defaults()
	c.Host = "vpn.example.com"
	c.Fallback = "203.0.113.10"
	c.VPNUser = "alice"
	c.VPNPass = "S3cure!!-testpass"
	c.Email = "admin@example.com"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := c.Remotes(); len(got) != 2 {
		t.Fatalf("want domain+IP fallback, got %v", got)
	}
}

func TestValidateRejects(t *testing.T) {
	c := Defaults()
	if err := c.Validate(); err == nil {
		t.Fatal("empty config must fail")
	}
	c = Defaults()
	c.Mode = ModeIP
	c.Host = "not-an-ip"
	c.VPNUser = "u"
	c.VPNPass = "long-enough-pass"
	if err := c.Validate(); err == nil {
		t.Fatal("bad IP must fail")
	}
}

func TestProtoAndAuthModes(t *testing.T) {
	c := Defaults()
	c.Host = "vpn.example.com"
	c.VPNUser = "alice"
	c.VPNPass = "S3cure!!-testpass"
	c.Email = "admin@example.com"
	c.Proto = "both"
	c.UdpPort = 1194
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := c.RemotesForProto("udp"); len(got) == 0 || !strings.Contains(got[0], "1194") {
		t.Fatalf("udp remotes must use udp-port, got %v", got)
	}
	// no-auth needs no password
	c.NoAuth = true
	c.VPNPass = ""
	if err := c.Validate(); err != nil {
		t.Fatal("no-auth should pass without password")
	}
	// both no-auth + no-password is contradictory
	c.NoPassword = true
	if err := c.Validate(); err == nil {
		t.Fatal("no-auth + no-password together must fail")
	}
}

func TestNormalizeFillsDefaults(t *testing.T) {
	var c Config
	c.Normalize()
	if c.Proto != "tcp" || c.Mtu != 1400 || c.Mss != 1200 || c.UdpPort != 1194 {
		t.Fatalf("normalize must fill proto/mtu/mss/udpport, got %+v", c)
	}
}
