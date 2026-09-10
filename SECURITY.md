# Security Policy

## Supported versions

The newest 1.x release is supported. Use tagged releases ([CHANGELOG](CHANGELOG.md) / [Releases](https://github.com/xdlc-labs/airlock/releases)).

Security fixes land on `main` and ship in the next tagged release. Untagged `main` / `go install @latest` may differ from a release binary.

## Reporting a vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Preferred:

1. Use GitHub **Security Advisories** (Private vulnerability reporting) on this repository once enabled.
2. Or email the maintainer listed on the GitHub profile that owns the repo, subject `Airlock security`.

Include:

- Affected command / package path
- Reproduction steps (PoC preferred)
- Impact (data leak, trust bypass, CI gate bypass, etc.)

You should get an acknowledgement within a few days. Please give a reasonable window before public disclosure.

## Security model (OSS CLI)

- **Local-first:** the OSS binary does not upload traces or eval data.
- **Redaction:** `ingest` / `baseline` run local regex redaction before writing under `.airlock/`.
- **Trust boundary:** treat `.airlock/` contents and eval fixtures as sensitive if they came from production.
- **Approvals:** `NEEDS_APPROVAL` blocks a merge when `fail_on.approval` is set in `.airlock/policy.yml` or CI passes `--fail-on-approval`; the GitHub Action passes that flag. Skill and MCP expansions both raise approval. On GitHub the sign-off is an approving pull request review on the head commit from a reviewer with write access, read over the API with the workflow token; a review on an older commit, from the author, or from a read-only account does not count. The local ledger (`airlock approve`) is the alternative and lives in the tree, so protect `.airlock/approvals/` with `CODEOWNERS` if you commit it.
- **Stdio MCP probing:** `--mcp-stdio` (and the Action's `mcp-stdio` input) runs the commands in the repository's MCP config. Leave it off for workflows that build pull requests from forks.

Known non-goals for the OSS CLI: multi-tenant auth, remote policy sync, guaranteed GDPR tooling.
