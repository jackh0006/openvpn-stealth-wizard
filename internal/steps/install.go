package steps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jackh0006/openvpn-stealth-wizard/internal/cfg"
	"github.com/jackh0006/openvpn-stealth-wizard/internal/logx"
)

const (
	serverDir  = "/etc/openvpn/server"
	usersDir   = "/etc/openvpn/users"
	authScript = "/etc/openvpn/check-pass.sh"
)

// PKI creates CA + server + client certificates with openssl and a
// tls-crypt key via openvpn. Skips anything already present.
type PKI struct{}

func (PKI) ID() string    { return "pki" }
func (PKI) Title() string { return "Build secret keys (PKI)" }
func (PKI) Why() string {
	return "Secret keys are like toothbrushes: the server and each phone get their own, never shared."
}

func (PKI) Check(c cfg.Config) Result {
	for _, f := range []string{"ca.crt", "server.crt", "server.key", "tls-crypt.key"} {
		if !fileExists(filepath.Join(serverDir, f)) {
			return Result{false, "missing " + f}
		}
	}
	return Result{true, "CA + server keys present"}
}

func (PKI) Apply(ctx context.Context, c cfg.Config, log *logx.Logger) error {
	pki := "/etc/openvpn/wizard-pki"
	if err := os.MkdirAll(pki, 0o700); err != nil {
		return err
	}
	// 0750 so openvpn (dropping to nobody:nogroup) can still stat the dir
	// on restarts; keys themselves are 0640 root:nogroup (read before drop,
	// but reload-safe). Old 0700 broke reloads on some distros.
	if err := os.MkdirAll(serverDir, 0o750); err != nil {
		return err
	}
	_ = os.Chown(serverDir, 0, getNogroupGid())
	mk := func(src, dst string) {
		if !fileExists(dst) && fileExists(src) {
			if b, err := os.ReadFile(src); err == nil {
				_ = os.WriteFile(dst, b, 0o600)
				log.Dim("kept existing " + dst)
			}
		}
	}
	_ = mk
	if fileExists(filepath.Join(serverDir, "ca.crt")) {
		// Fix perms on re-run (old installs were 0600 root-only).
		fixPKIPerms(log)
		log.OK("PKI already exists, keeping it (perms fixed)")
		return nil
	}
	subj := func(cn string) string {
		return fmt.Sprintf("/CN=%s/O=CompanyVPN", cn)
	}
	run := func(name string, args ...string) error {
		log.Dim("$ " + name + " " + strings.Join(args, " "))
		return Run(ctx, log, name, args...)
	}
	if err := run("openssl", "genrsa", "-out", pki+"/ca.key", "2048"); err != nil {
		return err
	}
	if err := run("openssl", "req", "-x509", "-new", "-nodes", "-key", pki+"/ca.key",
		"-sha256", "-days", "3650", "-out", pki+"/ca.crt", "-subj", subj("company-ca")); err != nil {
		return err
	}
	if err := run("openssl", "genrsa", "-out", pki+"/server.key", "2048"); err != nil {
		return err
	}
	if err := run("openssl", "req", "-new", "-key", pki+"/server.key",
		"-out", pki+"/server.csr", "-subj", subj("server")); err != nil {
		return err
	}
	ext := "basicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n"
	if err := os.WriteFile(pki+"/server.ext", []byte(ext), 0o600); err != nil {
		return err
	}
	if err := run("openssl", "x509", "-req", "-in", pki+"/server.csr",
		"-CA", pki+"/ca.crt", "-CAkey", pki+"/ca.key", "-CAcreateserial",
		"-days", "825", "-sha256", "-extfile", pki+"/server.ext",
		"-out", pki+"/server.crt"); err != nil {
		return err
	}
	if err := run("openvpn", "--genkey", "secret", pki+"/tls-crypt.key"); err != nil {
		return err
	}
	if err := run("openssl", "genrsa", "-out", pki+"/client.key", "2048"); err != nil {
		return err
	}
	if err := run("openssl", "req", "-new", "-key", pki+"/client.key",
		"-out", pki+"/client.csr", "-subj", subj("client")); err != nil {
		return err
	}
	cext := "basicConstraints=CA:FALSE\nkeyUsage=digitalSignature\nextendedKeyUsage=clientAuth\n"
	if err := os.WriteFile(pki+"/client.ext", []byte(cext), 0o600); err != nil {
		return err
	}
	if err := run("openssl", "x509", "-req", "-in", pki+"/client.csr",
		"-CA", pki+"/ca.crt", "-CAkey", pki+"/ca.key", "-CAcreateserial",
		"-days", "825", "-sha256", "-extfile", pki+"/client.ext",
		"-out", pki+"/client.crt"); err != nil {
		return err
	}
	if err := run("openssl", "dhparam", "-out", filepath.Join(serverDir, "dh.pem"), "2048"); err != nil {
		return err
	}
	for _, f := range []string{"ca.crt", "server.crt", "server.key", "tls-crypt.key"} {
		b, err := os.ReadFile(pki + "/" + f)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(serverDir, f), b, 0o600); err != nil {
			return err
		}
	}
	fixPKIPerms(log)
	log.OK("fresh PKI installed in " + serverDir)
	return nil
}

// fixPKIPerms makes keys reload-safe: dir 0750, keys 0640 nogroup,
// ipp/status/log writable. Best-effort, never fails the install.
func fixPKIPerms(log *logx.Logger) {
	gid := getNogroupGid()
	_ = os.Chmod(serverDir, 0o750)
	_ = os.Chown(serverDir, 0, gid)
	for _, f := range []string{"ca.crt", "server.crt", "server.key", "tls-crypt.key", "dh.pem"} {
		p := filepath.Join(serverDir, f)
		if fileExists(p) {
			_ = os.Chmod(p, 0o640)
			_ = os.Chown(p, 0, gid)
		}
	}
	// ipp.txt + logs must be writable by nobody:nogroup after drop.
	for _, p := range []string{"/etc/openvpn/ipp.txt", "/var/log/openvpn-status.log", "/var/log/openvpn.log"} {
		if !fileExists(p) {
			_ = os.WriteFile(p, []byte{}, 0o664)
		}
		_ = os.Chown(p, 65534, gid) // 65534 = nobody on Debian/Ubuntu
		_ = os.Chmod(p, 0o664)
	}
}

func getNogroupGid() int {
	// nogroup is gid 65534 on Debian/Ubuntu. Fall back to 65534.
	out, err := exec.Command("getent", "group", "nogroup").Output()
	if err != nil {
		return 65534
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(parts) < 3 {
		return 65534
	}
	var gid int
	fmt.Sscanf(parts[2], "%d", &gid)
	if gid == 0 {
		return 65534
	}
	return gid
}

// ServerConf writes the stealth server config: TCP on 443 with
// port-share website camouflage and password auth via-file.
type ServerConf struct{}

func (ServerConf) ID() string    { return "server" }
func (ServerConf) Title() string { return "Write VPN server config" }
func (ServerConf) Why() string {
	return "One door (port 443) serves two guests: VPN apps go to the tunnel, browsers get a normal website."
}

func serverConfText(c cfg.Config) string {
	return serverConfTextForProto(c, "tcp")
}

// serverConfTextForProto builds one server config for tcp or udp.
// TCP gets port-share decoy; UDP gets explicit-exit-notify 1.
func serverConfTextForProto(c cfg.Config, proto string) string {
	c.Normalize()
	authBlock := ""
	// Auth modes (user chose: also allow no-auth):
	// - NoAuth: testing only, no cert verify tweak, no password.
	// - NoPassword (cert-only): require cert, no password.
	// - default: require cert + password via-file.
	verifyLine := "verify-client-cert require\n"
	if c.NoAuth {
		authBlock = "# NO-AUTH testing mode (INSECURE): anyone with the .ovpn can connect.\nclient-cert-not-required\n"
		verifyLine = ""
	} else if !c.NoPassword {
		authBlock = fmt.Sprintf("script-security 2\nauth-user-pass-verify %s via-file\nusername-as-common-name\nauth-nocache\n", authScript)
	}
	dns1, dns2 := c.DNS1, c.DNS2
	if dns1 == "" {
		dns1 = "1.1.1.1"
	}
	if dns2 == "" {
		dns2 = "1.0.0.1"
	}
	mtu, mss := c.Mtu, c.Mss
	if mtu == 0 {
		mtu = 1400
	}
	if mss == 0 {
		mss = 1200
	}
	port := c.Port
	protoLine := "proto tcp-server"
	portShare := "port-share 127.0.0.1 8443\n"
	exitNotify := "explicit-exit-notify 0"
	if proto == "udp" {
		port = c.UdpPort
		if port == 0 {
			port = 1194
		}
		protoLine = "proto udp"
		portShare = "# no port-share on UDP (TCP-only decoy)\n"
		exitNotify = "explicit-exit-notify 1"
	}
	// Android fix: block-outside-dns stops OS leaking DNS past the tunnel.
	// redirect-gateway def1 + ipv6 block stops IPv6 leaks on mobile carriers.
	return fmt.Sprintf(`# Generated by openvpn-stealth-wizard v0.5.0. Safe to re-run.
port %d
%s
dev tun
ca %s/ca.crt
cert %s/server.crt
key %s/server.key
dh %s/dh.pem
tls-crypt %s/tls-crypt.key
topology subnet
server %s %s
ifconfig-pool-persist /etc/openvpn/ipp.txt
route %s %s
push "redirect-gateway def1 bypass-dhcp"
push "redirect-gateway ipv6"
push "block-outside-dns"
push "dhcp-option DNS %s"
push "dhcp-option DNS %s"
data-ciphers AES-256-GCM:AES-128-GCM
data-ciphers-fallback AES-256-GCM
auth SHA256
tls-version-min 1.2
%ssndbuf 0
rcvbuf 0
tcp-nodelay
tun-mtu %d
mssfix %d
keepalive 10 60
persist-key
persist-tun
%suser nobody
group nogroup
remote-cert-tls client
max-clients 100
status /var/log/openvpn-status.log
log-append /var/log/openvpn.log
verb 3
%s
`, port, protoLine,
		serverDir, serverDir, serverDir, serverDir, serverDir,
		subnetIP(c.Subnet), subnetMask(c.Subnet), subnetIP(c.Subnet), subnetMask(c.Subnet),
		dns1, dns2, authBlock+verifyLine, mtu, mss, portShare, exitNotify)
}

func subnetIP(cidr string) string {
	if i := strings.Index(cidr, "/"); i > 0 {
		return cidr[:i]
	}
	return cidr
}

func subnetMask(cidr string) string {
	masks := map[string]string{"24": "255.255.255.0", "16": "255.255.0.0", "8": "255.0.0.0"}
	if i := strings.Index(cidr, "/"); i > 0 {
		if m, ok := masks[cidr[i+1:]]; ok {
			return m
		}
	}
	return "255.255.255.0"
}

func (ServerConf) Check(c cfg.Config) Result {
	c.Normalize()
	proto := c.EffectiveProto()
	// both = check both confs.
	if proto == "both" {
		for _, f := range []string{"server.conf", "server-udp.conf"} {
			b, err := os.ReadFile(filepath.Join(serverDir, f))
			if err != nil {
				return Result{false, f + " missing (both mode needs TCP+UDP)"}
			}
			if r := checkOneServerConf(string(b), c, f == "server-udp.conf"); !r.OK {
				return r
			}
		}
		return Result{true, "TCP+UDP server confs match wizard spec"}
	}
	confName := "server.conf"
	wantUDP := proto == "udp"
	// udp-only still uses server.conf (single instance). both uses split files.
	b, err := os.ReadFile(filepath.Join(serverDir, confName))
	if err != nil {
		return Result{false, "server.conf missing"}
	}
	return checkOneServerConf(string(b), c, wantUDP)
}

func checkOneServerConf(s string, c cfg.Config, isUDP bool) Result {
	wantPort := c.Port
	if isUDP {
		wantPort = c.UdpPort
	}
	must := []string{fmt.Sprintf("port %d", wantPort)}
	if !isUDP {
		must = append(must, "port-share 127.0.0.1 8443")
	}
	if c.NoAuth {
		if strings.Contains(s, "auth-user-pass-verify") {
			return Result{false, "server.conf has password auth but mode is no-auth"}
		}
		if !strings.Contains(s, "client-cert-not-required") {
			return Result{false, "server.conf lacks client-cert-not-required (no-auth)"}
		}
	} else if !c.NoPassword {
		must = append(must, "auth-user-pass-verify "+authScript+" via-file")
	} else {
		if strings.Contains(s, "auth-user-pass-verify") {
			return Result{false, "server.conf still has password auth (should be cert-only)"}
		}
	}
	// Traffic-critical pushes that older versions lacked.
	for _, want := range []string{`push "block-outside-dns"`, `push "redirect-gateway`, fmt.Sprintf("tun-mtu %d", effectiveMtu(c)), fmt.Sprintf("mssfix %d", effectiveMss(c))} {
		if !strings.Contains(s, want) {
			return Result{false, "server.conf lacks: " + want + " (re-run --fix)"}
		}
	}
	for _, want := range must {
		if !strings.Contains(s, want) {
			return Result{false, "server.conf lacks: " + want}
		}
	}
	return Result{true, "server.conf matches wizard spec"}
}

func effectiveMtu(c cfg.Config) int {
	if c.Mtu == 0 {
		return 1400
	}
	return c.Mtu
}

func effectiveMss(c cfg.Config) int {
	if c.Mss == 0 {
		return 1200
	}
	return c.Mss
}

func (ServerConf) Apply(ctx context.Context, c cfg.Config, log *logx.Logger) error {
	c.Normalize()
	proto := c.EffectiveProto()
	// Ensure log/ipp files exist with right perms before restart.
	fixPKIPerms(log)
	if proto == "both" {
		backup(filepath.Join(serverDir, "server.conf"), log)
		backup(filepath.Join(serverDir, "server-udp.conf"), log)
		if err := os.WriteFile(filepath.Join(serverDir, "server.conf"),
			[]byte(serverConfTextForProto(c, "tcp")), 0o640); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(serverDir, "server-udp.conf"),
			[]byte(serverConfTextForProto(c, "udp")), 0o640); err != nil {
			return err
		}
		_ = os.Chown(filepath.Join(serverDir, "server.conf"), 0, getNogroupGid())
		_ = os.Chown(filepath.Join(serverDir, "server-udp.conf"), 0, getNogroupGid())
		log.OK(fmt.Sprintf("server.conf (TCP %d + port-share) + server-udp.conf (UDP %d) written", c.Port, c.UdpPort))
		if err := Run(ctx, log, "systemctl", "restart", "openvpn-server@server"); err != nil {
			return err
		}
		if err := Run(ctx, log, "systemctl", "restart", "openvpn-server@server-udp"); err != nil {
			log.Dim("UDP instance failed to start (will retry on --fix): " + err.Error())
		}
		_ = Run(ctx, log, "systemctl", "enable", "openvpn-server@server")
		_ = Run(ctx, log, "systemctl", "enable", "openvpn-server@server-udp")
		return Run(ctx, log, "systemctl", "is-active", "openvpn-server@server")
	}
	// Single-proto: always server.conf (so old tooling keeps working).
	wantUDP := proto == "udp"
	backup(filepath.Join(serverDir, "server.conf"), log)
	// Clean up stale split file when switching back to single proto.
	if !wantUDP {
		_ = Run(ctx, log, "systemctl", "stop", "openvpn-server@server-udp")
		_ = os.Remove(filepath.Join(serverDir, "server-udp.conf"))
	}
	which := "tcp"
	if wantUDP {
		which = "udp"
	}
	if err := os.WriteFile(filepath.Join(serverDir, "server.conf"),
		[]byte(serverConfTextForProto(c, which)), 0o640); err != nil {
		return err
	}
	_ = os.Chown(filepath.Join(serverDir, "server.conf"), 0, getNogroupGid())
	log.OK("server.conf written (" + which + ")")
	if err := Run(ctx, log, "systemctl", "restart", "openvpn-server@server"); err != nil {
		return err
	}
	return Run(ctx, log, "systemctl", "is-active", "openvpn-server@server")
}

// WebCamouflage parks the nginx 443 stream (if any) and serves a real
// decoy website on 127.0.0.1:8443 behind port-share.
type WebCamouflage struct{}

func (WebCamouflage) ID() string    { return "web" }
func (WebCamouflage) Title() string { return "Website camouflage" }
func (WebCamouflage) Why() string {
	return "Anyone knocking on 443 with a browser sees a boring company page, not a VPN."
}

func (WebCamouflage) Check(c cfg.Config) Result {
	if !fileExists("/etc/nginx/sites-enabled/vpn-camouflage") {
		return Result{false, "camouflage site missing"}
	}
	return Result{true, "camouflage site present"}
}

func (WebCamouflage) Apply(ctx context.Context, c cfg.Config, log *logx.Logger) error {
	if data, err := os.ReadFile("/etc/nginx/nginx.conf"); err == nil {
		s := string(data)
		if strings.Contains(s, "include /etc/nginx/stream-sni.conf;") {
			backup("/etc/nginx/nginx.conf", log)
			s = strings.ReplaceAll(s, "include /etc/nginx/stream-sni.conf;",
				"#include /etc/nginx/stream-sni.conf; # parked by wizard (OpenVPN owns 443)")
			if err := os.WriteFile("/etc/nginx/nginx.conf", []byte(s), 0o644); err != nil {
				return err
			}
			log.Dim("parked nginx 443 stream")
		}
	}
	c.Normalize()
	cert := "/etc/letsencrypt/live/" + c.Host + "/fullchain.pem"
	key := "/etc/letsencrypt/live/" + c.Host + "/privkey.pem"
	// FIX: old code wrote LE paths even when the files didn't exist, so
	// `nginx -t` failed and the decoy (port-share target 127.0.0.1:8443)
	// never came up. Now we fall back to a self-signed cert that always
	// validates, and tell the user how to get a real cert in domain mode.
	if c.Mode == cfg.ModeIP || !fileExists(cert) || !fileExists(key) {
		log.Dim("no Let's Encrypt cert for " + c.Host + "; creating self-signed fallback so port-share never breaks")
		if err := ensureSelfSignedDecoy(log, c.Host); err != nil {
			log.Dim("self-signed fallback failed: " + err.Error())
		} else {
			cert = "/etc/openvpn/server/decoy.crt"
			key = "/etc/openvpn/server/decoy.key"
		}
		// If still missing (openssl failed), use snakeoil if present, else plain HTTP.
		if !fileExists(cert) {
			if fileExists("/etc/ssl/certs/ssl-cert-snakeoil.pem") {
				cert = "/etc/ssl/certs/ssl-cert-snakeoil.pem"
				key = "/etc/ssl/private/ssl-cert-snakeoil.key"
				log.Dim("using snakeoil placeholder cert")
			} else {
				log.Dim("no TLS cert at all — serving decoy on plain HTTP 8080 + stunnel-free TCP 8443 fallback")
				return writePlainDecoy(ctx, log, c)
			}
		}
		if c.Mode == cfg.ModeDomain {
			log.Info("%s", "TIP (domain mode): for a real browser-trusted cert run: sudo certbot --nginx -d "+c.Host+" --email "+c.Email+" --agree-tos --non-interactive; then sudo systemctl reload nginx")
			log.Info("%s", "Cloudflare guide: DNS-only (grey cloud) = VPN works. Proxied (orange cloud) = only website works, VPN breaks. Use grey cloud for the VPN hostname.")
		}
	}
	site := `server {
    listen 127.0.0.1:8443 ssl;
    server_name ` + c.Host + `;
    ssl_certificate ` + cert + `;
    ssl_certificate_key ` + key + `;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_prefer_server_ciphers on;
    root /var/www/html;
    index index.html;
    location / { try_files $uri $uri/ =404; }
}
`
	if err := os.WriteFile("/etc/nginx/sites-available/vpn-camouflage", []byte(site), 0o644); err != nil {
		return err
	}
	if err := os.Symlink("/etc/nginx/sites-available/vpn-camouflage",
		"/etc/nginx/sites-enabled/vpn-camouflage"); err != nil && !os.IsExist(err) {
		return err
	}
	page := `<!DOCTYPE html>
<html><head><title>Welcome</title></head>
<body style="font-family:sans-serif;max-width:640px;margin:60px auto;color:#333">
<h1>Welcome to our website</h1>
<p>Company portal. Please contact IT for access.</p>
</body></html>
`
	if !fileExists("/var/www/html/index.html") {
		_ = os.WriteFile("/var/www/html/index.html", []byte(page), 0o644)
	}
	if err := Run(ctx, log, "nginx", "-t"); err != nil {
		return err
	}
	return Run(ctx, log, "systemctl", "reload-or-restart", "nginx")
}

// ensureSelfSignedDecoy creates a 1-year self-signed cert for the decoy
// so nginx -t always passes even without Let's Encrypt.
func ensureSelfSignedDecoy(log *logx.Logger, host string) error {
	crt := "/etc/openvpn/server/decoy.crt"
	key := "/etc/openvpn/server/decoy.key"
	if fileExists(crt) && fileExists(key) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*1000000000)
	defer cancel()
	// openssl req -x509 self-signed, no password, 365 days.
	if err := Run(ctx, log, "openssl", "req", "-x509", "-nodes", "-days", "365",
		"-newkey", "rsa:2048", "-keyout", key, "-out", crt,
		"-subj", "/CN="+host+"/O=Decoy"); err != nil {
		return err
	}
	_ = os.Chmod(crt, 0o644)
	_ = os.Chmod(key, 0o600)
	return nil
}

// writePlainDecoy is the last-resort decoy when no TLS cert exists at all:
// plain HTTP on 127.0.0.1:8443 (OpenVPN port-share proxies HTTPS there,
// but plain HTTP still answers probes without crashing nginx).
func writePlainDecoy(ctx context.Context, log *logx.Logger, c cfg.Config) error {
	site := `server {
    listen 127.0.0.1:8443;
    server_name ` + c.Host + `;
    root /var/www/html;
    index index.html;
    location / { try_files $uri $uri/ =404; }
}
`
	if err := os.WriteFile("/etc/nginx/sites-available/vpn-camouflage", []byte(site), 0o644); err != nil {
		return err
	}
	_ = os.Symlink("/etc/nginx/sites-available/vpn-camouflage",
		"/etc/nginx/sites-enabled/vpn-camouflage")
	if err := Run(ctx, log, "nginx", "-t"); err != nil {
		return err
	}
	return Run(ctx, log, "systemctl", "reload-or-restart", "nginx")
}
