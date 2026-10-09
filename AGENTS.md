# SPDX-License-Identifier: MIT
# AGENTS.md — AI contributor guide for openvpn-stealth-wizard

You touch people's servers. Be conservative, be honest.

## Hard rules

1. **Never break a working wizard.** `gofmt`, `go vet`, `go test ./...`
   green before every commit. Prefer docs over code churn.
2. **No secrets in repo.** No keys, `.ovpn` contents, tokens. Test against
   throwaway VMs; `--check` mode changes nothing — keep it that way.
3. **Honest stealth language.** Decoy + SNI routing raise blocking cost;
   OpenVPN handshakes stay fingerprintable by real DPI. Never `unhackable /
   invisible / untraceable / NSA-proof`. The README ISP section is the
   source of truth — keep it accurate.
4. **Fail closed.** Firewall deny-by-default; no silent port opens; doctor
   reports problems instead of papering over them.
5. **Version-proof texts.** No hardcoded versions in README/docs (stable
   install URLs live in this file once). Releases cut only by tag via
   goreleaser with the canonical `Name-<ver>-<Os>-<Arch>` names.
6. **Keep MIT.** No license change without the owner's explicit sign-off.

## Workflow

- Small diffs, tests with behavior changes.
- End every answer with: files changed, how to verify, open risks.
  If you cannot follow a rule, stop and ask the human.
