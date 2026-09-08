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

5. Watch **Release** workflow (`.github/workflows/release.yml`). It builds linux/darwin × amd64/arm64, attaches tarballs + sha256, creates the GitHub Release (pre-release if the tag contains `beta` / `rc` / `alpha`). The workflow cannot list the Action on GitHub Marketplace.
6. Marketplace (org owner, 2FA, browser only). Edit the new release. Accept the GitHub Marketplace Developer Agreement if the checkbox is disabled. Tick **Publish this Action to the GitHub Marketplace**. Primary category: Continuous integration. Optional second: Code quality. Click **Update release**. Listing URL is derived from `action.yml` `name` (spaces become hyphens). Current `name:` is `Airlock AI release gate` (`/marketplace/actions/airlock-ai-release-gate`). `uses:` stays `xdlc-labs/airlock@<tag>`. Do not set `name:` to `Airlock`. That login is taken by [github.com/airlock](https://github.com/airlock), and GitHub refuses the listing.
7. Smoke:

```bash
curl -sSL https://raw.githubusercontent.com/xdlc-labs/airlock/main/install.sh | AIRLOCK_VERSION=vX.Y.Z bash
airlock version
```

## Moving `v0` tag

Every popular Action offers a major-version tag so users can write
`uses: xdlc-labs/airlock@v0` and pick up patches without editing a workflow.
Airlock only publishes pre-releases today, so GitHub's "latest" link skips them
and a reader who copies from the Marketplace gets nothing. Move `v0` after each
cut:

```bash
git tag -f v0 vX.Y.Z-beta.N
git push -f origin v0
```

`action.yml` resolves the CLI from `github.action_ref`, and its release-download
branch only triggers on a `v[0-9]*` ref. `v0` matches, so the tag must point at a
commit whose release assets exist — that is, an already-published release, not a
branch head. Build-from-source is the fallback if a download 404s, so a stale
`v0` degrades rather than breaking.

## Manual / dry-run

Actions → **Release** → **Run workflow** → optional `tag` input. Prefer a real tag push for production cuts.

## What not to do

- Do not attach hand-built binaries that skip `-ldflags "-X main.version=…"`.
- Do not use tags without a `v` prefix — `install.sh` and the workflow both expect `v*`.
- Do not spray the new version across GUIDE / SUPPORT / SECURITY — link [CHANGELOG](../CHANGELOG.md) instead.
- Do not ship with an empty or “Unreleased-only” changelog — betas need readable Release notes.
