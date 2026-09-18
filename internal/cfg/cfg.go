package cfg

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Mode selects IP-literal or domain operation.
type Mode string

const (
	ModeIP     Mode = "ip"
	ModeDomain Mode = "domain"
)

// Config holds every wizard input. It mirrors the runbook we validated
// on a live Ubuntu 22.04 VPS.
type Config struct {
	Mode     Mode
	Host     string // IP or domain, used in `remote` lines
	Fallback string // raw IP fallback appended as second remote (DNS-bypass)
	Port     int
	VPNUser  string
	VPNPass  string
	Email    string // Let's Encrypt contact (domain mode)
	Subnet   string // e.g. 10.8.0.0/24
	OutDir   string // client bundle dir, e.g. /root/Open Code/OpenVPN
}

func Defaults() Config {
	return Config{
		Mode:   ModeDomain,
		Port:   443,
		Subnet: "10.8.0.0/24",
		OutDir: "/root/Open Code/OpenVPN",
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
	if len(c.VPNPass) < 8 {
		return errors.New("vpn password must be at least 8 characters")
	}
	if _, _, err := net.ParseCIDR(c.Subnet); err != nil {
		return fmt.Errorf("bad subnet %q: %w", c.Subnet, err)
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

// Remotes renders primary + fallback remote lines.
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
