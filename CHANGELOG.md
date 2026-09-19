# Changelog — newest on top, plain words.

## v0.5.1 (2026-09-19) — boot fix (VPS-tasted)
- FIXED critical: sndbuf typo (ndbuf broke OpenVPN 2.5 start) + auth-before-server order (check-pass.sh missing on first boot). VPS-tasted: both TCP:443 + UDP:1194 active, tunnel ping OK, doctor all green.

## v0.5.0 (2026-09-19) — traffic fix + smart wizard
- FIXED no-traffic bug (connects but 0 bytes): auto-detect WAN (was hardcoded enp1s0), persistent ip_forward via sysctl.d, UFW allow inbound + tun+/WAN forwarding, FORWARD ACCEPT fallback, MSS clamp on tun+ AND egress, block-outside-dns + IPv6 redirect pushes, decoy self-signed fallback so nginx never breaks port-share, PKI perms 0750/0640 reload-safe
- New: --proto tcp|udp|both + --port + --udp-port (stealth TCP 443 with decoy, fast UDP, or both with 2 .ovpn files). TUI ctrl+p cycles.
- New: 3 login modes — password+cert (default), --no-password cert-only, --no-auth testing-only (insecure, needs --yes-i-know-insecure, TUI ctrl+o). TUI ctrl+n/ctrl+o.
- New: --doctor smart diagnosis (read-only, AI-like hints) + --dns + --mtu/--mss flags, 12 health probes (services, ports, NAT, forwarding, MSS, conf, DNS, decoy, bundle)
- New: Cloudflare guide everywhere (grey cloud DNS-only = VPN works, orange cloud proxied = VPN breaks) + --fallback DNS-bypass
- Fixed: deps auto-install (ca-certificates check, apt-get update args, certbot lazy for domain mode)
- Fixed: uninstall cleans tun+/all NICs + server-udp.conf
- New: dependencies auto-install (openvpn, nginx, iptables, ufw, curl …) — no manual apt needed
- New: full uninstall (TUI uninstall mode, --uninstall / --manage uninstall, backup kept, SSH never touched)

## v0.4.0 (2026-09-18)
- Synced to VPS-tasted working state: tun-mtu 1400 / mssfix 1200 on server + bundles
- New: DNS choice (cloudflare/google/quad9/adguard/custom) with TUI field + headless --dns
- New: TCPMSS clamp on tun0 both ways (fixes stale-phone files) + cert-only mode (--no-password/--gen-pass)
- Fixed: manage delete verified + live log streaming, port freeRetry

## v0.3.2 (2026-09-18)
- Fixed: manage delete now verified (conf gone or error) + daemon-reload, with live log backup path
- Smart: self-owned 443 handling already in 0.3.1, now with robust ps-based unit detection + retry

## v0.3.1 (2026-09-18)
- Fixed: installing over your own VPN on 443 no longer blocks — review says “will restart in place”, enter proceeds (port is freed by restart)

## v0.3.0 (2026-09-18)
- VPS: restored 01-JH + built missing 02-JH (IP-fallback scp now works)
- Fixed: port 443 freeing with retry loop + real unit detection, always asks first
- New: install asks password type — type it / suggest strong / cert-only (--no-password)
- Fixed: manage delete is now 9 in a clear numbered menu (was hidden x)
- Fixed: bundle detection + download shows IP-based scp with password/key help + live logs streaming (3)

## v0.2.1 (2026-09-18)
- Fixed: port freeing now finds the real systemd unit (even for openvpn),
  always asks before touching a self-owned port, one-press free + continue
- Fixed: manage now shows a clear numbered menu (1-9) with delete on 9,
  bundle detection no longer hardcoded to one username
- New: suggested password (ctrl+g) in the form and after add-user, with
  auto-fill if left empty
- New: done screen prints exact scp download line and phone import guide
  so beginners know how to get the .ovpn

## v0.2.0 (2026-09-18)
- Port takeover: wizard names the process holding your port and offers to
  free it (TUI `f` key, headless `--free-port`); port 22/SSH always refused
- Manage mode: list/restart/delete servers, users add/change/delete,
  revoke client certificates, live clients, logs, full backup
- Headless `--manage` actions mirror everything for scripts
- Delete = full purge with timestamped backup tarball first

## v0.1.2 (2026-09-18)
- Fixed: form fields are editable now (typing reaches the inputs)
- Fixed: a second bug where the form never saved (validator tested)
- Back navigation on every screen (esc), two-press stop during runs
- Smarter: public-IP auto-detect for fallback, remembers last run,
  per-field error hints, port-taken warning in plan preview
- Wording: plain professional language throughout

## v0.1.1 (2026-09-18)
- One-line installer: `curl ... install.sh | sudo bash` (chip auto-detect,
  installs as `wizard`, proves itself with `--version`)
- New `--help` flag with examples a beginner can follow
- Donate table with the family crypto wallets
- Docs: fixed install links (tarballs, not bare filenames) + `chmod +x` reminder

## v0.1.0
- First release: guided Bubble Tea wizard (install / check / fix)
- 8 runbook steps: preflight, conflicts, PKI, server.conf (TCP 443 +
  port-share + `via-file` password auth), decoy website, users, NAT+UFW+
  return route, client bundle with DNS-bypass fallback
- Read-only `--check` health report, headless flags, linux amd64+arm64
