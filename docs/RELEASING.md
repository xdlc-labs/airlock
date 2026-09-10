# Releasing Airlock

Cut a tagged GitHub Release. `install.sh` downloads assets named:

`airlock_<version>_<os>_<arch>.tar.gz` (e.g. `airlock_1.0.0_linux_amd64.tar.gz`), and `airlock_<version>_windows_<arch>.zip` for Windows.

Version history for users: [CHANGELOG.md](../CHANGELOG.md). The **only** doc that should carry a concrete install pin for end users is [README — Install](../README.md#install) (update it when you cut a release).

## Checklist

1. CI green on `main` (`.github/workflows/ci.yml`).
2. Update [CHANGELOG.md](../CHANGELOG.md):
   - Move items from **Unreleased** under the new version heading (`Added` / `Changed` / `Fixed`).
   - Keep a short **Highlights** + **Known limits** block when useful.
   - Update compare links at the bottom of `CHANGELOG.md`.
3. Update the install pin in [README.md](../README.md) (`AIRLOCK_VERSION=…` / `go install @…`) — nowhere else. Leave `uses: xdlc-labs/airlock@v1` unless you are moving the major tag.
4. Tag and push (must start with `v`):

```bash
git checkout main
git pull
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

5. Watch **Release** workflow (`.github/workflows/release.yml`). It builds linux/darwin/windows × amd64/arm64, attaches tarballs (zip for Windows) + sha256, creates the GitHub Release (pre-release if the tag contains `beta` / `rc` / `alpha`). The workflow cannot list the Action on GitHub Marketplace.
6. Marketplace (org owner, 2FA, browser only). Edit the new release. Accept the GitHub Marketplace Developer Agreement if the checkbox is disabled. Tick **Publish this Action to the GitHub Marketplace**. Primary category: Continuous integration. Optional second: Code quality. Click **Update release**. Listing URL is derived from `action.yml` `name` (spaces become hyphens). Current `name:` is `Airlock AI release gate` (`/marketplace/actions/airlock-ai-release-gate`). App repos pin `uses: xdlc-labs/airlock@v1`, or an exact tag. Do not set `name:` to `Airlock`. That login is taken by [github.com/airlock](https://github.com/airlock), and GitHub refuses the listing.
7. Smoke:

```bash
curl -sSL https://raw.githubusercontent.com/xdlc-labs/airlock/main/install.sh | AIRLOCK_VERSION=vX.Y.Z bash
airlock version
```

## Moving `v1` tag

Every popular Action offers a major-version tag so users can write
`uses: xdlc-labs/airlock@v1` and pick up patches without editing a workflow.
Move `v1` after each 1.x cut:

```bash
git tag -f v1 vX.Y.Z
git push -f origin v1
```

`action.yml` resolves a major-only ref by asking the API for the newest release
whose tag starts with it, so `v1` does not have to name a release itself — it only
has to point at a commit whose `action.yml` carries that resolution step. Keep
pointing it at a published release anyway, so what `@v1` runs is a version you
actually shipped. Build-from-source stays the fallback when no release matches,
so a stale `v1` degrades rather than breaking. The old `v0` tag stays where it
is: repos that pinned it keep the last 0.x release until they move.

## Manual / dry-run

Actions → **Release** → **Run workflow** → optional `tag` input. Prefer a real tag push for production cuts.

## What not to do

- Do not attach hand-built binaries that skip `-ldflags "-X main.version=…"`.
- Do not use tags without a `v` prefix — `install.sh` and the workflow both expect `v*`.
- Do not spray the new version across SUPPORT / SECURITY. Link [CHANGELOG](../CHANGELOG.md) instead.
- Do not ship with an empty or “Unreleased-only” changelog — releases need readable notes.
