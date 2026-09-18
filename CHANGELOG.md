# Changelog — newest on top, plain words.

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
