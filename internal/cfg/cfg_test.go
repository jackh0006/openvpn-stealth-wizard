package cfg

import "testing"

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
