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
	Port       int    // TCP port (stealth default 443)
	Proto      string // tcp | udp | both (default tcp). udp uses UdpPort.
	UdpPort    int    // UDP port when Proto is udp or both (default 1194)
	VPNUser    string
	VPNPass    string
	NoPassword bool // when true: cert-only, no password required
	NoAuth     bool // when true: NO auth at all (testing only, insecure!)
	Email      string // Let's Encrypt contact (domain mode)
	Subnet     string // e.g. 10.8.0.0/24
	OutDir     string // client bundle dir, e.g. /root/Open Code/OpenVPN
	DNS1       string // primary pushed DNS
	DNS2       string // secondary pushed DNS
	Mtu        int    // tun-mtu (default 1400, LTE-safe)
	Mss        int    // TCPMSS clamp (default 1200)
}

func Defaults() Config {
	return Config{
		Mode:    ModeDomain,
		Port:    443,
		Proto:   "tcp",
		UdpPort: 1194,
		Subnet:  "10.8.0.0/24",
		OutDir:  "/root/Open Code/OpenVPN",
		DNS1:    "1.1.1.1",
		DNS2:    "1.0.0.1",
		Mtu:     1400,
		Mss:     1200,
	}
}

// Validate explains problems like a patient teacher.
func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port %d out of range (1-65535... use 1-65535)", c.Port)
	}
	if c.UdpPort < 1 || c.UdpPort > 65535 {
		return fmt.Errorf("udp-port %d out of range (1-65535)", c.UdpPort)
	}
	switch c.Proto {
	case "", "tcp", "udp", "both":
	default:
		return fmt.Errorf("proto %q must be tcp, udp or both", c.Proto)
	}
	if c.Proto == "" {
		c.Proto = "tcp"
	}
	if c.NoAuth && c.NoPassword {
		return errors.New("pick only one: --no-auth (no login at all) or --no-password (cert-only), not both")
	}
	if strings.TrimSpace(c.VPNUser) == "" {
		return errors.New("vpn username is empty")
	}
	if c.NoAuth {
		// testing-only mode: no password needed at all.
	} else if !c.NoPassword && len(c.VPNPass) < 8 {
		return errors.New("vpn password must be at least 8 characters (or choose --no-password cert-only, or --no-auth testing-only)")
	}
	if c.Mtu != 0 && (c.Mtu < 1200 || c.Mtu > 1500) {
		return fmt.Errorf("mtu %d out of range (1200-1500, recommended 1400 for LTE)", c.Mtu)
	}
	if c.Mss != 0 && (c.Mss < 1000 || c.Mss > 1460) {
		return fmt.Errorf("mss %d out of range (1000-1460, recommended 1200)", c.Mss)
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
	c.Normalize()
	return c, true
}
// Normalize fills zero-values from Defaults (for old last.json files).
func (c *Config) Normalize() {
	d := Defaults()
	if c.Proto == "" {
		c.Proto = d.Proto
	}
	if c.UdpPort == 0 {
		c.UdpPort = d.UdpPort
	}
	if c.Mtu == 0 {
		c.Mtu = d.Mtu
	}
	if c.Mss == 0 {
		c.Mss = d.Mss
	}
	if c.Subnet == "" {
		c.Subnet = d.Subnet
	}
	if c.OutDir == "" {
		c.OutDir = d.OutDir
	}
	if c.DNS1 == "" {
		c.DNS1 = d.DNS1
	}
	if c.DNS2 == "" {
		c.DNS2 = d.DNS2
	}
	if c.Port == 0 {
		c.Port = d.Port
	}
}

func (c Config) Remotes() []string {
	// Default: TCP remotes (backwards compatible). For udp/both callers
	// use RemotesForProto. Proto "" means tcp.
	return c.RemotesForProto("tcp")
}

// RemotesForProto returns `remote` lines for tcp or udp.
func (c Config) RemotesForProto(proto string) []string {
	port := c.Port
	if proto == "udp" {
		port = c.UdpPort
		if port == 0 {
			port = 1194
		}
	}
	// OpenVPN client uses `proto tcp-client` / `udp`, but the `remote`
	// line itself is just host+port. We keep proto suffix out for compat
	// with older bundles; proto is set by the `proto` directive.
	out := []string{fmt.Sprintf("remote %s %s", c.Host, strconv.Itoa(port))}
	fb := c.Fallback
	if c.Mode == ModeIP {
		fb = ""
	}
	if fb != "" && fb != c.Host {
		out = append(out, fmt.Sprintf("remote %s %s", fb, strconv.Itoa(port)))
	}
	return out
}

// EffectiveProto normalises empty to tcp.
func (c Config) EffectiveProto() string {
	if c.Proto == "" {
		return "tcp"
	}
	return c.Proto
}
