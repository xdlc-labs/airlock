# Releasing Airlock

Cut a tagged GitHub Release. `install.sh` downloads assets named:

`airlock_<version>_<os>_<arch>.tar.gz` (e.g. `airlock_0.1.0_linux_amd64.tar.gz`)

Version history for users: [CHANGELOG.md](../CHANGELOG.md). The **only** doc that should carry a concrete install pin for end users is [README — Install](../README.md#install) (update it when you cut a release).

## Checklist

1. CI green on `main` (`.github/workflows/ci.yml`).
2. Update [CHANGELOG.md](../CHANGELOG.md):
   - Move items from **Unreleased** under the new version heading (`Added` / `Changed` / `Fixed`).
   - For prereleases, keep a short **Highlights** + **Known limits** block when useful.
   - Update compare links at the bottom of `CHANGELOG.md`.
3. Update the install pin in [README.md](../README.md) (`AIRLOCK_VERSION=…` / `go install @…` / `uses: xdlc-labs/airlock@…`) — nowhere else.
4. Tag and push (must start with `v`):

```bash
git checkout main
git pull
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

5. Watch **Release** workflow (`.github/workflows/release.yml`). It builds linux/darwin × amd64/arm64, attaches tarballs + sha256, creates the GitHub Release (pre-release if the tag contains `beta` / `rc` / `alpha`).
6. Smoke:

```bash
curl -sSL https://raw.githubusercontent.com/xdlc-labs/airlock/main/install.sh | AIRLOCK_VERSION=vX.Y.Z bash
airlock version
```

## Manual / dry-run

Actions → **Release** → **Run workflow** → optional `tag` input. Prefer a real tag push for production cuts.

## What not to do

- Do not attach hand-built binaries that skip `-ldflags "-X main.version=…"`.
- Do not use tags without a `v` prefix — `install.sh` and the workflow both expect `v*`.
- Do not spray the new version across GUIDE / SUPPORT / SECURITY — link [CHANGELOG](../CHANGELOG.md) instead.
- Do not ship with an empty or “Unreleased-only” changelog — betas need readable Release notes.
