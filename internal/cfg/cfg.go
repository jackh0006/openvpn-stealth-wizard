package cfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Mode selects IP-literal or domain operation.
type Mode string

const (
	ModeIP     Mode = "ip"
	ModeDomain Mode = "domain"
)

// DNSPresets is the Big 4 + custom choice.
var DNSPresets = map[string][2]string{
	"cloudflare": {"1.1.1.1", "1.0.0.1"},
	"google":     {"8.8.8.8", "8.8.4.4"},
	"quad9":      {"9.9.9.9", "149.112.112.112"},
	"adguard":    {"94.140.14.14", "94.140.15.15"},
}

// Config holds every wizard input. It mirrors the runbook we validated
// on a live Ubuntu 22.04 VPS.
type Config struct {
	Mode       Mode
	Host       string // IP or domain, used in `remote` lines
	Fallback   string // raw IP fallback appended as second remote (DNS-bypass)
	Port       int
	VPNUser    string
	VPNPass    string
	NoPassword bool   // when true: cert-only, no password required
	Email      string // Let's Encrypt contact (domain mode)
	Subnet     string // e.g. 10.8.0.0/24
	OutDir     string // client bundle dir, e.g. /root/Open Code/OpenVPN
	DNS1       string // primary pushed DNS
	DNS2       string // secondary pushed DNS
}

func Defaults() Config {
	return Config{
		Mode:   ModeDomain,
		Port:   443,
		Subnet: "10.8.0.0/24",
		OutDir: "/root/Open Code/OpenVPN",
		DNS1:   "1.1.1.1",
		DNS2:   "1.0.0.1",
	}
}

// Validate explains problems like a patient teacher.
func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port %d out of range (1-65565... use 1-65535)", c.Port)
	}
	if strings.TrimSpace(c.VPNUser) == "" {
		return errors.New("vpn username is empty")
	}
	if !c.NoPassword && len(c.VPNPass) < 8 {
		return errors.New("vpn password must be at least 8 characters (or choose cert-only)")
	}
	if _, _, err := net.ParseCIDR(c.Subnet); err != nil {
		return fmt.Errorf("bad subnet %q: %w", c.Subnet, err)
	}
	if c.DNS1 != "" && net.ParseIP(c.DNS1) == nil {
		return fmt.Errorf("dns1 %q is not an IP", c.DNS1)
	}
	if c.DNS2 != "" && net.ParseIP(c.DNS2) == nil {
		return fmt.Errorf("dns2 %q is not an IP", c.DNS2)
	}
	switch c.Mode {
	case ModeIP:
		if net.ParseIP(c.Host) == nil {
			return fmt.Errorf("mode=ip but host %q is not an IP", c.Host)
		}
	case ModeDomain:
		if strings.Contains(c.Host, " ") || !strings.Contains(c.Host, ".") {
			return fmt.Errorf("mode=domain but host %q looks wrong", c.Host)
		}
		if c.Fallback != "" && net.ParseIP(c.Fallback) == nil {
			return fmt.Errorf("fallback %q is not an IP", c.Fallback)
		}
		if !strings.Contains(c.Email, "@") {
			return errors.New("email needed for Let's Encrypt (domain mode)")
		}
	default:
		return fmt.Errorf("unknown mode %q", c.Mode)
	}
	return nil
}

// lastRunPath is where the wizard remembers accepted inputs.
func lastRunPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "openvpn-stealth-wizard", "last.json")
}

// SaveLast remembers this config for next time.
func (c Config) SaveLast() {
	p := lastRunPath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(p, b, 0o600)
}

// LoadLast returns the previous run's config, if any.
func LoadLast() (Config, bool) {
	p := lastRunPath()
	if p == "" {
		return Config{}, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Config{}, false
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, false
	}
	return c, true
}
func (c Config) Remotes() []string {
	out := []string{fmt.Sprintf("remote %s %s", c.Host, strconv.Itoa(c.Port))}
	fb := c.Fallback
	if c.Mode == ModeIP {
		fb = ""
	}
	if fb != "" && fb != c.Host {
		out = append(out, fmt.Sprintf("remote %s %s", fb, strconv.Itoa(c.Port)))
	}
	return out
}
