# SPDX-License-Identifier: MIT
# OpenVPN Stealth Wizard — Privacy Policy

This tool configures **your** server. It collects nothing for the author.

## What leaves the device

- **Your server IP + DNS**: the wizard reads the public IP to write configs;
  domain mode publishes a DNS `A` record. Both are public by design.
- **Package mirrors + Go proxy** (builds only): see IPs + requested modules.
- **Let's Encrypt** (domain mode): domain validation; certs are public in
  Certificate Transparency logs.

## What stays local

- OpenVPN PKI, `tls-crypt-v2` keys, `.ovpn` files — `0600`, never committed.
- No analytics, no telemetry, no phone-home in `cmd/` or `internal/`.

## Operator notes

- Your VPS provider sees sizes/timing/destinations — a VPN hides content
  from local networks, not activity from the provider. Real DPI can
  fingerprint OpenVPN handshakes (see README "What the ISP sees").
- Carrier DNS blocking is countered with a raw-IP fallback you provide.

Questions: open an issue (no keys, no `.ovpn` contents).
