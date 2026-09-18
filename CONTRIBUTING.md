# Contributing

Keep it small, safe, and tested — this tool runs as root on servers.

1. One step = one file section in `internal/steps/`: implement
   `Check` (read-only) + `Apply` (idempotent, backs up before writing).
2. Every scary lesson gets a regression test (`*_test.go`).
3. Run before pushing: `gofmt -l .`, `go vet ./...`, `go test ./...`.
4. Never commit secrets, `.ovpn` files, keys, passwords, or real IPs —
   tests and docs use `example.com` / `203.0.0.0/24` placeholders only.
5. Releases: tag `vX.Y.Z`, GoReleaser builds linux amd64 + arm64.
