# Contributing to Airlock

Keep changes small and testable. By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## Setup

```bash
git clone https://github.com/xdlc-labs/airlock.git
cd airlock
go test ./... -count=1
go build -o airlock ./cmd/airlock
```

Requires Go 1.25+. Optional: [golangci-lint](https://golangci-lint.run/) v2.

## Pull requests

1. Branch from `main`.
2. One concern per PR. Add tests for non-trivial logic.
3. Run `go test ./... -count=1 -race` and `go build -o airlock ./cmd/airlock`.
4. Do not commit `.airlock/`, built binaries, or secrets.
5. Security issues go to [SECURITY.md](SECURITY.md), not a public issue.

| Good fits | Open an issue first |
|-----------|---------------------|
| Bug fixes, docs, fixtures | Hosted control plane / SSO |
| Eval, policy, stats | Autonomic rollback agent |
| Discovery and CI gate hardening | Replacing APM or AppSec scanners |

Airlock **imports** APM lockfiles. It does not re-implement APM resolution.

Maintainers: [docs/RELEASING.md](docs/RELEASING.md).

## License

Contributions are licensed under the [Apache License 2.0](LICENSE).
