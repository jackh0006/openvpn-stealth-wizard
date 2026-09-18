# Security Policy

## Supported Versions

| Version | Supported |
| ------- | --------- |
| 0.1.x   | Yes       |

## Reporting a Vulnerability

Open a **private** GitHub Security Advisory on this repo
(Security tab → Advisories → New draft advisory). Do **not** open a public
issue for vulnerabilities.

Please include: wizard version (`wizard --version`), server OS, steps to
reproduce, and logs with secrets redacted. We aim to acknowledge within
72 hours.

## Operator Safety Notes

- Never commit real `.ovpn` files, private keys, or passwords. The wizard
  only ever writes those to your server's `/etc/openvpn` and bundle dir.
- Rotate any credential that was ever pasted into a chat or log.
- `--check` is read-only and safe to run anywhere; install modes need root
  on the target server only.
