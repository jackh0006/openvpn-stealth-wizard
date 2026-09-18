# Changelog — newest on top, plain words.

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
