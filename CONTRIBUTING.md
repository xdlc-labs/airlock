# Contributing to Airlock

Thanks for considering a contribution. Keep changes small and testable.

## Code of Conduct

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## Before you open an issue

1. Search existing issues.
2. Reproduce on latest `main` with `go test ./...` / a minimal CLI repro.
3. For security issues, follow [SECURITY.md](SECURITY.md). Never file those publicly.

## Development setup

```bash
git clone https://github.com/xdlc-labs/airlock.git
cd airlock
go test ./... -count=1
go build -o airlock ./cmd/airlock
```

Requires Go 1.25+. Optional: [golangci-lint](https://golangci-lint.run/) v2 (`golangci-lint run ./...`).

## Pull requests

1. Fork and branch from `main` (`feat/…`, `fix/…`).
2. One concern per PR.
3. Add or update tests for non-trivial logic.
4. Run before push:

```bash
go test ./... -count=1 -race
golangci-lint run ./...
go build -o airlock ./cmd/airlock
```

5. Fill the PR template: **why**, what changed, how you tested.
6. Do not commit `.airlock/`, built `airlock` binaries, or secrets.

## Releases

Maintainers: see [docs/RELEASING.md](docs/RELEASING.md). Tag `vX.Y.Z` on `main` (pattern `v*.*.*`, so the moving `v1` tag does not publish a release) → Release workflow publishes binaries for `install.sh`.

## Scope guidance

| Good fits | Usually out of scope (open an issue first) |
|-----------|--------------------------------------------|
| Bug fixes, docs, fixtures | Hosted control plane / SSO |
| Eval / policy / stats improvements | Autonomic rollback agent |
| Discovery: skills, Cursor rules, APM / Promptfoo / OTel | Competing with APM install-time features |
| CI gate / approval hardening | Every framework plugin at once, unit-test selection for app CI |

Airlock **imports** APM lockfiles. It does not re-implement APM resolution. A hosted control plane, SSO, and an autonomic rollback agent are out of scope for this repository; open an issue before starting on anything of that shape.

## License

Contributions are licensed under the project [Apache License 2.0](LICENSE).
