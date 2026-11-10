# Contributing

Bug reports and focused pull requests are welcome. This edition targets Claude Code, Codex and Senpi-based OMO, with Windows/WSL desktop notifications and WeCom. For changes to that scope, open an issue first and refer to [ADR 0001](docs/adr/0001-first-release-scope.md).

## Local checks

Use Go 1.25+, Node.js 22+ for the Senpi extension contract test, and Python 3 for packaging. Runtime users do not need these build tools.

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/agent-notify
```

Format changed Go files with `gofmt`. Use `make package VERSION=v0.1.0` (or `python scripts/package.py --version v0.1.0`) to build both Windows architectures as ZIP and both Linux architectures as tar.gz. Check `dist/SHA256SUMS` and run the extracted native binary with `--version` and `--help`.

Tests use temporary configuration and local HTTP servers. Do not send test messages to real groups or modify your live agent hooks for automated checks. On a real Windows/WSL desktop, manually verify `test system` when changing toast behavior; cross-compilation and mocked PowerShell tests cannot prove the toast appeared.

To exercise first delivery and agent branding on a WSL desktop, run `AGENT_NOTIFY_TEST_WINDOWS=1 go test ./internal/notify -run 'TestDesktop(FirstNotification|AgentBranding)OnWindows' -count=1 -v`. In Windows PowerShell, set `$env:AGENT_NOTIFY_TEST_WINDOWS = '1'` before the same `go test` command. These opt-in checks submit local toasts with fresh app identities, verify registered names and icon files (including paths with Unicode and special characters), then remove their temporary notification history, registry entries and assets. Other test runs skip them.

## Changes

- Preserve unrelated agent settings during installation and uninstall.
- Keep hooks noninteractive and bounded by timeouts. Notification failures must be observable without failing the calling agent.
- Treat webhook URLs, raw configurations and backups as secrets. Do not include them in logs, fixtures, issues or pull requests.
- Cover behavior changes through public commands or module interfaces and keep both READMEs current.
- Keep the original MIT copyright when redistributing derived code.

## Release maintenance

The repository has its own version series, separate from upstream. After the main-branch checks pass, create and push an unused `vX.Y.Z` tag. The Release workflow tests on Linux and Windows, builds four standalone archives using `scripts/package.py`, creates checksums and publishes a GitHub Release. Push only the intended release tag, not all locally inherited upstream tags.
