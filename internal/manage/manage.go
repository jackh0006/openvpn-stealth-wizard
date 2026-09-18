package manage

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Paths are vars so tests can redirect them to temp dirs.
var (
	ServerDir       = "/etc/openvpn/server"
	UsersDir        = "/etc/openvpn/users"
	BundleRoot      = "/root/Open Code/OpenVPN"
	LogFile         = "/var/log/openvpn.log"
	StatusDir       = "/run/openvpn-server"
	LegacyStatusLog = "/var/log/openvpn-status.log"
)

// Server is one discovered VPN instance.
type Server struct {
	Name    string
	Port    int
	Subnet  string
	Active  bool
	Clients []string
	Bundle  bool
}

func serviceActive(unit string) bool {
	b, err := exec.Command("systemctl", "is-active", unit).Output()
	return err == nil && strings.TrimSpace(string(b)) == "active"
}

func confVal(body, key string) string {
	for _, ln := range strings.Split(body, "\n") {
		f := strings.Fields(strings.TrimSpace(ln))
		if len(f) >= 2 && f[0] == key {
			return f[1]
		}
	}
	return ""
}

// Discover lists every server.conf under ServerDir (read-only).
func Discover() []Server {
	entries, err := os.ReadDir(ServerDir)
	if err != nil {
		return nil
	}
	var out []Server
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".conf") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".conf")
		b, err := os.ReadFile(filepath.Join(ServerDir, e.Name()))
		if err != nil {
			continue
		}
		s := Server{Name: name}
		fmt.Sscanf(confVal(string(b), "port"), "%d", &s.Port)
		for _, ln := range strings.Split(string(b), "\n") {
			if f := strings.Fields(strings.TrimSpace(ln)); len(f) == 3 && f[0] == "server" {
				s.Subnet = f[1] + " " + f[2]
				break
			}
		}
		s.Active = serviceActive("openvpn-server@" + name)
		s.Clients = ConnectedClients(name)
		s.Bundle = fileExists(filepath.Join(BundleRoot, name+".ovpn"))
		if !s.Bundle {
			if entries, _ := os.ReadDir(BundleRoot); len(entries) > 0 {
				for _, e := range entries {
					if strings.HasSuffix(e.Name(), ".ovpn") {
						s.Bundle = true
						break
					}
				}
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ConnectedClients parses the status log for live client names.
func ConnectedClients(name string) []string {
	paths := []string{StatusDir + "/status-" + name + ".log", LegacyStatusLog}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var clients []string
		inList := false
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(ln, "Common Name,") {
				inList = true
				continue
			}
			if strings.HasPrefix(ln, "ROUTING TABLE") {
				break
			}
			if inList && strings.TrimSpace(ln) != "" {
				clients = append(clients, strings.Split(ln, ",")[0])
			}
		}
		return clients
	}
	return nil
}

// Restart bounces one server.
func Restart(name string) error {
	if out, err := exec.Command("systemctl", "restart", "openvpn-server@"+name).CombinedOutput(); err != nil {
		return fmt.Errorf("restart %s: %v: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func backupTar(paths []string) (string, error) {
	var present []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			present = append(present, p)
		}
	}
	if len(present) == 0 {
		return "", fmt.Errorf("nothing to back up")
	}
	dst := fmt.Sprintf("/root/openvpn-backup-%s.tar.gz", time.Now().Format("20060102-150405"))
	args := append([]string{"-czf", dst}, present...)
	if out, err := exec.Command("tar", args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("tar: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return dst, nil
}

// Delete purges a server: stop+disable, backup tarball first, then
// remove the server conf. Keeps users/bundles on disk (they are in the
// backup) but server disappears from Discover immediately. Verified.
func Delete(name string) (string, error) {
	_ = exec.Command("systemctl", "stop", "openvpn-server@"+name).Run()
	_ = exec.Command("systemctl", "disable", "openvpn-server@"+name).Run()
	conf := filepath.Join(ServerDir, name+".conf")
	bak, err := backupTar([]string{
		conf,
		ServerDir + "/ca.crt", ServerDir + "/server.crt",
		ServerDir + "/server.key", ServerDir + "/tls-crypt.key",
		UsersDir, BundleRoot,
	})
	if err != nil {
		// Even if backup fails, still try to remove the conf — user asked.
		_ = os.Remove(conf)
		if _, e2 := os.Stat(conf); e2 == nil {
			return "", fmt.Errorf("backup failed: %v (conf still present)", err)
		}
		return "", err
	}
	if err := os.Remove(conf); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("remove %s: %w (backup at %s)", conf, err, bak)
	}
	if _, err := os.Stat(conf); err == nil {
		return "", fmt.Errorf("delete failed: %s still exists after remove (backup at %s)", conf, bak)
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return bak, nil
}

// ListUsers returns VPN usernames.
func ListUsers() []string {
	entries, err := os.ReadDir(UsersDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// SetUser creates or changes a password (min 8 chars). Fixes nobody perms.
func SetUser(user, pass string) error {
	if len(pass) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if strings.ContainsAny(user, "/ \t\n") || user == "" {
		return fmt.Errorf("bad username %q", user)
	}
	if err := os.MkdirAll(UsersDir, 0o755); err != nil {
		return err
	}
	p := filepath.Join(UsersDir, user)
	if err := os.WriteFile(p, []byte(pass), 0o644); err != nil {
		return err
	}
	_ = exec.Command("chown", "nobody:nogroup", p).Run()
	return nil
}

// DelUser removes a login. Active sessions drop on next re-auth/restart.
func DelUser(user string) error {
	p := filepath.Join(UsersDir, user)
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("user %q does not exist", user)
	}
	return os.Remove(p)
}

// RevokeClient removes a client certificate. With easy-rsa it also
// regenerates the CRL; otherwise it deletes the cert files and notes
// that a server restart applies it.
func RevokeClient(cn string) (string, error) {
	easyrsa := "/etc/openvpn/easy-rsa/easyrsa"
	if _, err := os.Stat(easyrsa); err == nil {
		out, err := exec.Command(easyrsa, "revoke", cn).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("revoke: %v: %s", err, strings.TrimSpace(string(out)))
		}
		gen, err := exec.Command(easyrsa, "gen-crl").CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("gen-crl: %v: %s", err, strings.TrimSpace(string(gen)))
		}
		return "revoked + CRL regenerated (restart server to enforce)", nil
	}
	removed := 0
	for _, p := range []string{
		"/etc/openvpn/wizard-pki/" + cn + ".crt",
		"/etc/openvpn/wizard-pki/" + cn + ".key",
		"/etc/openvpn/easy-rsa/pki/issued/" + cn + ".crt",
		"/etc/openvpn/easy-rsa/pki/private/" + cn + ".key",
	} {
		if err := os.Remove(p); err == nil {
			removed++
		}
	}
	if removed == 0 {
		return "", fmt.Errorf("no certificate files found for %q", cn)
	}
	return fmt.Sprintf("removed %d cert files (restart server to enforce)", removed), nil
}

// TailLog returns the last n lines of the VPN log (read-only).
func TailLog(n int) string {
	b, err := os.ReadFile(LogFile)
	if err != nil {
		return "log unreadable: " + err.Error()
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// BackupAll archives the whole VPN estate.
func BackupAll() (string, error) {
	return backupTar([]string{"/etc/openvpn", "/etc/nginx/sites-available/vpn-camouflage", BundleRoot, LogFile})
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
