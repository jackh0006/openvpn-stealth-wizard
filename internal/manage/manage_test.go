package manage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempRoot(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	ServerDir = filepath.Join(d, "server")
	UsersDir = filepath.Join(d, "users")
	BundleRoot = filepath.Join(d, "bundle")
	LogFile = filepath.Join(d, "openvpn.log")
	t.Cleanup(func() {
		ServerDir = "/etc/openvpn/server"
		UsersDir = "/etc/openvpn/users"
		BundleRoot = "/root/Open Code/OpenVPN"
		LogFile = "/var/log/openvpn.log"
	})
	return d
}

func TestDiscoverParsesConf(t *testing.T) {
	d := tempRoot(t)
	os.MkdirAll(ServerDir, 0o755)
	os.WriteFile(filepath.Join(ServerDir, "office.conf"), []byte("port 443\nserver 10.9.0.0 255.255.255.0\n"), 0o600)
	got := Discover()
	if len(got) != 1 || got[0].Name != "office" || got[0].Port != 443 {
		t.Fatalf("bad discover: %+v", got)
	}
	if !strings.Contains(got[0].Subnet, "10.9.0.0") {
		t.Fatalf("bad subnet: %q", got[0].Subnet)
	}
	_ = d
}

func TestUserRoundTrip(t *testing.T) {
	tempRoot(t)
	if err := SetUser("alice", "S3cure!!-pass"); err != nil {
		t.Fatal(err)
	}
	if users := ListUsers(); len(users) != 1 || users[0] != "alice" {
		t.Fatalf("bad list: %v", users)
	}
	if err := SetUser("alice", "N3w-pass-word"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(UsersDir, "alice"))
	if string(b) != "N3w-pass-word" {
		t.Fatal("password change did not stick")
	}
	if err := SetUser("bob", "short"); err == nil {
		t.Fatal("short password must fail")
	}
	if err := DelUser("alice"); err != nil {
		t.Fatal(err)
	}
	if err := DelUser("ghost"); err == nil {
		t.Fatal("deleting missing user must fail")
	}
}

func TestConnectedClientsParsesStatus(t *testing.T) {
	d := tempRoot(t)
	StatusDir = filepath.Join(d, "status")
	LegacyStatusLog = filepath.Join(d, "legacy.log")
	t.Cleanup(func() {
		StatusDir = "/run/openvpn-server"
		LegacyStatusLog = "/var/log/openvpn-status.log"
	})
	os.MkdirAll(StatusDir, 0o755)
	b := "TITLE,OpenVPN\nCommon Name,Real Address,Bytes Received\n01-JH,1.2.3.4:5,10\nalice,5.6.7.8:9,20\nROUTING TABLE\n"
	os.WriteFile(filepath.Join(StatusDir, "status-office.log"), []byte(b), 0o644)
	got := ConnectedClients("office")
	if len(got) != 2 || got[0] != "01-JH" || got[1] != "alice" {
		t.Fatalf("bad clients: %v", got)
	}
	if got := ConnectedClients("nosuch"); got != nil {
		t.Fatalf("missing log must give nil, got %v", got)
	}
}
