# openvpn-stealth-wizard

![linux-amd64](https://img.shields.io/badge/linux-amd64-42b983)
![linux-arm64](https://img.shields.io/badge/linux-arm64-42b983)
![license](https://img.shields.io/badge/license-MIT-blue)

Beautiful terminal wizard that builds a **stealth OpenVPN server**: VPN on TCP
port 443 with a real decoy website on the same port (port-share), password
login, Cloudflare DNS, and an import-ready `.ovpn` with DNS-bypass fallback.

Born from a real deployment runbook. Every lesson is now code.

## Installation (one command)

On your Ubuntu server, as root, paste this **one line** and press Enter:

```sh
curl -sSL https://raw.githubusercontent.com/jackh0006/openvpn-stealth-wizard/main/install.sh | sudo bash
```

That's it. It finds your chip (amd64/arm64), downloads the newest version,
installs it as `wizard`, and proves it works. Then:

```sh
sudo wizard          # pretty guided setup (recommended)
wizard --help        # every command with examples
```

Manual way (if you don't trust pipes):

```sh
# amd64 (most VPS servers) — file inside is named `wizard`
curl -sSL -o vpn.tar.gz https://github.com/jackh0006/openvpn-stealth-wizard/releases/latest/download/openvpn-stealth-wizard_0.1.0_linux_amd64.tar.gz
# arm64 (Ampere / Pi servers): same link with _arm64.tar.gz
tar xzf vpn.tar.gz && chmod +x wizard && sudo mv wizard /usr/local/bin/
wizard --version
```

> Verify checksums against `checksums.txt` from the same release page.

Prerequisites: **Ubuntu 22.04/24.04**, **root**, a public IP, and (domain
mode) a DNS `A` record pointing at the server. If your mobile carrier blocks
the domain's DNS (common!), the wizard adds a raw-IP fallback automatically —
give it `--fallback YOUR_SERVER_IP`.

## Quick start (2 minutes to first green check)

```sh
# 1. Taste it — read-only health check, changes NOTHING (safe anywhere)
./wizard --check --mode domain --host vpn.example.com \
  --user alice --pass 'secret' --email admin@example.com
# ✔ / ✘ table + exit code: 0 = healthy, 1 = something missing

# 2. Guided install — run with no flags for the pretty TUI
sudo ./wizard
# mode → inputs (prefilled) → plan preview → live logs → health check → done card

# 3. Scripted install — same thing, no questions asked
sudo ./wizard --non-interactive --yes \
  --mode domain --host vpn.example.com --fallback 203.0.113.10 \
  --port 443 --user alice --pass 'S3cure!!' --email admin@example.com

# 4. Repair later — re-applies only missing pieces
sudo ./wizard --non-interactive --yes --fix ... (same flags)
```

IP-only? Swap `--mode domain --host vpn.example.com` for
`--mode ip --host 203.0.113.10` (no email needed). Custom port? Change
`--port` (443 recommended: looks like a normal website).

## Manage existing servers (TUI `manage` mode or flags)

```sh
sudo wizard --manage list                                   # all servers + live clients
sudo wizard --manage user-add --muser bob --mpass 'S3cure!!'
sudo wizard --manage user-pass --muser bob --mpass 'N3w-pass!!'
sudo wizard --manage user-del --muser bob
sudo wizard --manage revoke --muser oldphone                # kill a lost cert
sudo wizard --manage restart --server server
sudo wizard --manage delete --server server                 # full purge, backup first
sudo wizard --manage backup                                 # one tarball of everything
```

Port taken? The wizard names the owner and offers to free it (`f` in the
TUI, `--free-port` headless). SSH (port 22) is never touched.

## Guided TUI tour

No flags at all → gorgeous step-by-step TUI: mode → inputs → plan preview →
live logs → health check → done card. Every screen explains *why* like you're
five, with live logs streaming each command as it runs.

## What it does (8 steps, our scars included)

1. **Preflight** — Ubuntu + root + binaries present
2. **Conflicts** — finds nginx/stunnel fights over 443 before they bite
3. **PKI** — openssl CA/server/client certs + `tls-crypt`, keeps existing
4. **Server** — TCP + `port-share` to nginx + `via-file` password auth
   (never `via-env`: systemd-sandboxed daemons lose env vars — we learned hard)
5. **Web** — parks nginx 443 stream, serves decoy site on 127.0.0.1:8443
6. **Auth** — `nobody`-readable user passwords, tested auth script
7. **Net** — forwarding, NAT masquerade (persisted), UFW routes,
   **kernel return route into tun0** (the silent killer: without it the
   tunnel connects but zero bytes return)
8. **Client** — `.ovpn` with domain + raw-IP fallback remotes, fast failover

Plus a strict rule the wizard enforces: **never run a redirect-gateway
self-test on the server itself** (it hijacks the default route and locks you out).

## What the ISP sees

TCP to your-IP:443, sizes/timing, TLS handshake **without SNI** —
fingerprintable as OpenVPN by real DPI. Contents (AES-256-GCM), DNS
(Cloudflare through the tunnel), and app identity stay hidden. Active
probers get the decoy website. For DPI-proof stealth, wrap in
stunnel/Shadowsocks (Phase 2).

## Build & release

```sh
go test ./...            # unit tests (templates, order, validation)
go vet ./...
GOOS=linux GOARCH=amd64 go build -o wizard-linux-amd64 ./cmd/wizard
GOOS=linux GOARCH=arm64  go build -o wizard-linux-arm64 ./cmd/wizard
goreleaser release       # see .goreleaser.yml
```

Linux amd64 + arm64 only (v1).

## Donate (crypto)

If this wizard saved you an evening, tips keep the releases coming.
Same wallets as my other projects:

| Coin | Address |
| ---- | ------- |
| BTC | `bc1q8t0fn2yrsy4lh3m0pz34uj27t8vxjeavkjym83` |
| DOGE | `D6ZdMQ7mHGGmuH9prpZ2zjpnG5Q3WVRDtC` |
| ETH | `0xdad428900a4359be8f76b3062df34211582e09eb` |
| USDT (ERC-20) | `0xdad428900a4359be8f76b3062df34211582e09eb` |
| BNB / USDT (BEP-20) | `0xdad428900a4359be8f76b3062df34211582e09eb` |
| TRX / USDT (TRC-20) | `TMpb6RNTuGNM1eTakm9kjds1mRTPYYJesf` |
| SOL / USDT+USDC (SPL) | `BDCCrRez1yD1RpkAtiqKKDk3BfxPD8P7nkL26jCYrzgL` |
| XRP | `rNUAhaATFLvosdu9m9M95bupRBtZ8eqpj9` |
| TON | `UQCu6-3yGyQ5dzvcCxr2gobuvx5ddbS9EC690qtey92P5_wX` |
| LTC | `ltc1q2gs89cfy3mumr7gu9w0zl9rllf80q67m5rmma8` |

Verify addresses against `.github/FUNDING.yml` (or the Sponsor button) —
never trust an address pasted anywhere else.
