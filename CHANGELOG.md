# Changelog

All notable changes to Airlock are documented here.

Format inspired by [Keep a Changelog](https://keepachangelog.com/).
Versions follow [SemVer](https://semver.org/) with prerelease tags (`beta`, `rc`).

## [Unreleased]

### Added

### Changed

### Fixed

## [0.1.0-beta.9] – 2026-09-07

GitHub Marketplace listing for the reusable Action. Pin `uses: xdlc-labs/airlock@v0.1.0-beta.9`.

### Highlights
- Boxed CLI cards on the terminal. GitHub alert PR comments.
- Action downloads the release binary on a `v*` tag instead of compiling Go on every PR.

### Changed
- Daily CLI commands (`init`, `snapshot`, `diff`, `test`, `ci`, `approve`) write boxed cards to stderr. Color is TTY-only. GitHub Actions gets `::notice::` / `::warning::` / `::error::` for the verdict.
- `airlock ci --comment` no longer prints the PR body on stdout. `.airlock/ci-comment.md` is always written.
- PR comments use GitHub alerts (`TIP` / `WARNING` / `CAUTION` / `NOTE`), a one-row summary table, and `+` / `~` / `-` change marks.
- GitHub Action prints a numbered 1/5 setup trail, groups noisy steps, posts the comment file (update or create by marker), and copies that comment into the job summary. Downloads a release binary when the action ref is a `v*` tag.
- `action.yml` has Marketplace branding (`shield` / `blue`).

## [0.1.0-beta.8] – 2026-09-07

### Fixed
- Snapshot IDs no longer hash `generated_at` or the absolute `root` path. CI was minting a new base id every run, so `airlock approve --base` from the PR comment could not unblock the next job.

## [0.1.0-beta.7] – 2026-09-07

Reusable GitHub Action: checkout, then `uses: xdlc-labs/airlock@v0.1.0-beta.7`.

### Added
- Root `action.yml`. Builds the CLI, diffs merge-base vs HEAD, comments on the PR, fail-closed on approval.
- `airlock ci --fail-on-inconclusive`: `--fail-on-eval` alone only ever tripped CI on `FAIL`, so default thresholds (`0.99`/`0.995` min) against the default `max_samples_per_case: 5` could sit at `INCONCLUSIVE` indefinitely with nothing failing the build. The new flag fails closed on `INCONCLUSIVE` too (and still fails on `FAIL` when enabled alone).
- Comparative eval gates (`task_success` regression, `adversarial_critical`) now show a `SKIPPED` row with reason when no baseline result exists to compare against, instead of silently vanishing from the report with zero trace. `SKIPPED` never fails closed on its own.
- Skill hashing now covers the whole skill directory (`manifest.HashDirTree`), not just `SKILL.md` — a sibling script/resource changing without touching `SKILL.md` used to go undetected; it now registers as a skill change.

### Changed
- This repo’s workflow is `uses: ./`. App repos pin a release tag.

### Fixed

## [0.1.0-beta.6] – 2026-09-02

Pre-Phase-5 hardening: clearer PR comments, tighter blast-radius/permission-expansion story — the two items named in [ROADMAP — Next proof](docs/ROADMAP.md#next-proof-not-a-big-saas).

### Added
- PR comment now shows eval blast radius (not just agents) and a gate `reason` column alongside the verdict.
- PR comment carries the exact `airlock approve --base --head` unblock command when `NEEDS_APPROVAL`, instead of leaving it in CI logs; omitted once already approved.
- `airlock approve` prints the pending reasons before recording the ledger entry.
- MCP servers carry `ToolNames` from the live `tools/list` fetch (HTTP(S) only); `airlock diff`/`ci` now diffs that set directly, so a genuinely new tool on a live server raises `NEEDS_APPROVAL` even when `apm.lock.yaml`'s `permissions:` was never hand-maintained.

### Changed
- Docs: sharper wedge — OSS AI release gate now; team/enterprise control plane Phases 5–6; release agent Phase 7; explicit non-goal for app CI test selection ([ROADMAP](docs/ROADMAP.md), README, GUIDE).
- Dropped `"post"` from the write-tool name heuristic — false-positived on read-only names like `post_processing_helper`.

### Fixed
- Sample GitHub Action skipped posting the PR comment whenever the fail-closed gate itself failed (`if: always()` was missing) — the exact PR a blocked-merge explanation matters most for got a silent red X and nothing else.
- `WithNeedsApproval` no longer clobbers a real eval-`FAIL` summary with the approval note; the actual blocking reason now stays visible alongside it.

## [0.1.0-beta.5] – 2026-08-25

Phase 4 complete: eval flexibility + lockfile supply chain.

### Added
- **Eval flexibility:** `.airlock/eval-bindings.yml` artifact→suite binding in `airlock ci`; experiment compare table vs baseline in CI/`airlock test`; `airlock eval promote --from ingest|results`; `import langsmith|braintrust`; multi-turn judge `turns` templates.
- **Lockfile supply chain:** `go.sum`, `package-lock.json`, `Cargo.lock` read directly into `manifest.Dependency`.

## [0.1.0-beta.4] – 2026-08-25

Phase 4.1 Model Sentinel + Phase 4.2 OpenAI/LangGraph stack scanner.

### Added
- **Model Sentinel** (`airlock sentinel probe|check`): fingerprint upstream models with a fixed probe prompt; detect silent provider drift when the config model string is unchanged. `--fail-on-sentinel` on `airlock ci`; `airlock snapshot --sentinel` folds fingerprints into the manifest.
- **OpenAI / LangGraph stack scanner**: discover `ChatOpenAI` / `model=` strings in Python, TS/JS, and Go source; LangGraph imports tagged as `langgraph-scan`.
- **Live MCP schema fetch**: HTTP(S) MCP servers get `tools/list` schema hashed at scan time (`+mcp-live` source tag); stdio servers stay config-hash only.

## [0.1.0-beta.3] – 2026-08-24

Phase 4 roadmap item shipped early, plus a merge-gate bypass fix found while building its demo.

### Added
- Agent-driven supply chain: APM package dependencies tracked as `manifest.Dependency`; a new dependency landing alongside an AI-artifact change (prompt/skill/MCP/agent) raises `NEEDS_APPROVAL` in `airlock diff` / `airlock ci --fail-on-approval`. Dependency-only PRs are left to SCA.

### Fixed
- `airlock ci --comment` no longer bypasses `--fail-on-approval` / `--fail-on-eval` / `fail_on_ai_change` — it now only changes the stdout format and still writes `ci-comment.md`; the fail-closed gate always runs.

## [0.1.0-beta.2] – 2026-08-21

Re-cut of the first public beta with working GitHub Release assets (beta.1 publish raced and left an empty release).

### Changed
- README quick start: console walkthrough (prompt change → diff → CI comment; MCP → `NEEDS_APPROVAL`)

### Fixed
- Publish a complete multi-arch release for `install.sh` (linux/darwin × amd64/arm64)

Install from this tag (not beta.1). One pin lives in [README — Install](README.md#install).

## [0.1.0-beta.1] – 2026-08-21

**First public beta** of the local-first Airlock CLI: AI release control for git repos (manifest → snapshot → diff → statistical eval → CI gate).

### Highlights

- Treat models, prompts, tools, **skills**, MCP servers, judges, and evals as one releasable unit
- Content-addressed snapshots, blast-radius diff, policy verdicts (`PASS` / `FAIL` / `INCONCLUSIVE` / `NEEDS_APPROVAL`)
- PR-oriented `airlock ci` with optional fail-closed flags and sample GitHub Actions workflow
- Local-first store under `.airlock/` — no telemetry

### Added

#### Discovery & manifest

- Import [Microsoft APM](https://github.com/microsoft/apm) lockfiles (`apm.lock.yaml` / `apm.yml`)
- Scan prompts, MCP configs, env trees, model-string heuristics
- First-class **`skill`** artifacts (APM skills are skills, not tools)
- Discover Agent Skills: `SKILL.md` under `.claude/skills`, `.agents/skills`, `.gemini/skills`
- Discover Cursor rules (`.cursor/rules/*.mdc`, `*.md`) as prompts (`source: cursor-rules`)

#### Release loop

- `init` / `snapshot` / `diff` / `history` (optional local `--serve` UI)
- Statistical eval runner with Wilson/bootstrap CIs, budgets, cassettes (replay) and live modes
- `import promptfoo`, OTel ingest + redaction, baselines, `drift`
- Judge registry: calibrate / attribution
- `approve` / `rollback` + gateway routing hints

#### CI & security gates

- Adversarial / injection suites; MCP **or skill** changes auto-prefer adversarial cases
- Permission expansion → `NEEDS_APPROVAL`: MCP permission growth, write/unknown tools, **skill add/change**
- `data_boundary.fail_on_pii` on model I/O
- Sample app workflow [`.github/workflows/airlock.yml`](.github/workflows/airlock.yml): defaults `AIRLOCK_FAIL_ON_APPROVAL=true`
- Multi-arch GitHub Release assets for `install.sh` (linux/darwin × amd64/arm64)

#### Docs & fixtures

- [README](README.md), [GUIDE](docs/GUIDE.md) (incl. MCP approval demo + Security in CI), [RELEASING](docs/RELEASING.md)
- Toy agent under `testdata/toy-agent` (prompts, skill, Cursor rule, evals, APM lock)

### Known limits (honest)

- No hosted control plane / SSO / K8s admission (Phases 5–6)
- Approvals are advisory unless CI passes `--fail-on-approval`
- Windows install not supported yet

[Unreleased]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.9...HEAD
[0.1.0-beta.9]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.8...v0.1.0-beta.9
[0.1.0-beta.8]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.7...v0.1.0-beta.8
[0.1.0-beta.7]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.6...v0.1.0-beta.7
[0.1.0-beta.6]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.5...v0.1.0-beta.6
[0.1.0-beta.5]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.4...v0.1.0-beta.5
[0.1.0-beta.4]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.3...v0.1.0-beta.4
[0.1.0-beta.3]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.2...v0.1.0-beta.3
[0.1.0-beta.2]: https://github.com/xdlc-labs/airlock/releases/tag/v0.1.0-beta.2
[0.1.0-beta.1]: https://github.com/xdlc-labs/airlock/releases/tag/v0.1.0-beta.1
