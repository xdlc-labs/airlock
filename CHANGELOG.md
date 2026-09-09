# Changelog

All notable changes to Airlock are documented here.

Format inspired by [Keep a Changelog](https://keepachangelog.com/).
Versions follow [SemVer](https://semver.org/) with prerelease tags (`beta`, `rc`).

## [Unreleased]

### Added
- Wiring an existing capability into an agent now raises `NEEDS_APPROVAL`. Adding an MCP server, write tool, skill, or a different model to an agent leaves that artifact's own hash untouched, so the only previous signal was the agent hash moving with no reason attached. Agents added in the same diff are left to the existing per-artifact reasons instead of enumerating every link.
- Changed artifacts that no agent declares are reported as an unknown blast radius (`unlinked_changes` in the JSON, an `unknown` line in the terminal report, a note in the PR comment). A file-scanned prompt or Cursor rule that nothing links to used to render as "agents: none linked", which reads as "affects nothing" when it actually means eval selection could not narrow to it.
- Agent instruction files are discovered as prompts: `CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `.cursorrules`, `.windsurfrules`, `.github/copilot-instructions.md`, `.github/instructions/*.md`, and `.claude/agents/*.md`, nested copies included. These reach the model on every turn and are the files people actually edit, and until now editing one produced no diff at all. Vendored and fixture copies (`node_modules`, `testdata`, `.venv`, `third_party`, …) are skipped, and a file already declared in `apm.lock.yaml` is not duplicated.
- MCP configs written in the VS Code and Zed style (`servers` / `context_servers` instead of `mcpServers`) are read per server, so their servers get an artifact each, a live tool list, and a permission-expansion signal. Previously such a file was hashed whole, which caught an edit but could not say which server changed. `.windsurf/mcp.json`, `.gemini/settings.json`, and `.zed/settings.json` are new config locations.
- `.airlock/policy.yml` gained a `fail_on:` block (`approval`, `eval`, `inconclusive`, `sentinel`, `ai_change`), so a repo keeps its gates on without every workflow repeating the `--fail-on-*` flags. `airlock init` writes it fail-closed for new repos. A flag still forces a gate on; policy never switches one off. Repos whose policy predates the block behave exactly as before and get one warning per run saying the gate reports but cannot block.
- `airlock import langsmith --dataset NAME|ID` pulls a LangSmith dataset over the API instead of requiring a hand-made export, with `--limit N` to cap it and paging through everything otherwise. The key comes from `LANGSMITH_API_KEY` (or `LANGCHAIN_API_KEY`); `--api-url` or `LANGSMITH_ENDPOINT` points at a self-hosted deployment. Cases are written to `.airlock/evals/langsmith.jsonl` by the same conversion the file import uses, so both produce identical cases for the same examples. Datasets only: traces and online eval scores stay in LangSmith.
- `--mcp-stdio` on `init`, `snapshot`, `diff`, and `ci` reads the live `tools/list` of MCP servers configured with a `command`, so a stdio server growing a new write-looking tool now raises `NEEDS_APPROVAL` the way an http(s) server already did. Probing spawns the server, so it is off by default and prints a warning when enabled: the command comes from the repository under test, which makes it unsafe on workflows that build pull requests from forks. Probe on a machine you trust and commit the manifest to give CI the tool list without CI starting anything. Unprobed stdio servers keep their config hash exactly as before, and probes are bounded by a 20s timeout with the server killed afterwards.

### Changed
- The PR comment leads with what to do. When a change needs sign-off, the reasons and the `airlock approve` command now sit directly under the verdict instead of below the eval tables, where a reviewer had to scroll past the evidence to find the one command that unblocks the merge. The change table gained `where` and `hash` columns, so `~ prompt system-prompt` reads as `prompts/system.md` with the hash it moved from and to, and a folded Snapshots block names the two snapshots compared with the `airlock diff` that reproduces the comparison locally.
- The PR comment is bounded: at most 30 change rows (with a count of what was left out) and a hard clamp at GitHub's 65536-byte comment limit. A gate report over that limit was rejected by the API, so a large PR could produce no comment at all.
- `fail_on_ai_change` is still honored, now read through the policy loader rather than a hand-rolled line scanner in `store`. `fail_on.ai_change` wins when both are set.

### Fixed

## [0.1.0-beta.14] – 2026-09-09

Discovers six more agent frameworks and more model families. Also scans pnpm,
yarn, and Python lockfiles. Pin `uses: xdlc-labs/airlock@v0` or
`@v0.1.0-beta.14`.

### Highlights
- CrewAI, AutoGen, LlamaIndex, the Vercel AI SDK, and the Anthropic SDK are tagged, not only the OpenAI SDK and LangGraph.
- Mistral, Llama, DeepSeek, Qwen, Grok, and similar family ids are recognized.
- Lockfiles for pnpm, yarn, Poetry, Pipenv, and uv feed the same supply-chain gate as `go.sum` / `package-lock.json` / `Cargo.lock`.

### Known limits
- Windows install not supported yet
- Comparative gates still report `SKIPPED` until a baseline exists
- Heuristics, not a full AST per language. Go SDKs that name models with exported constants stay invisible.

### Added
- Framework detection beyond the OpenAI SDK and LangGraph: the Anthropic SDK, LlamaIndex, CrewAI, AutoGen, and the Vercel AI SDK. Each source file is tagged with the framework it uses (`anthropic-sdk-scan`, `llamaindex-scan`, `crewai-scan`, `autogen-scan`, `vercel-ai-scan`), and a framework is recorded even when it names no model string of its own. Models named positionally, as the Vercel AI SDK does with `anthropic("claude-haiku-4-5")`, are now discovered too.
- Model ids beyond the OpenAI, Anthropic, and Google names are recognized and attributed to a provider: Mistral (including Mixtral, Codestral, Devstral, Magistral, Pixtral), Llama, DeepSeek, Qwen, Grok, Cohere Command, GLM, Kimi, Gemma, Phi, and Amazon Nova. A family name must carry a version marker to count, so `llama-3.3-70b` is read as a model and `llama_index` stays a package. A bare family name (`mistral`) is not enough: write the id the provider serves.
- Lockfile discovery for `pnpm-lock.yaml`, `yarn.lock` (classic and Berry), `poetry.lock`, `Pipfile.lock`, and `uv.lock`. They feed the same agent-driven supply-chain gate as `go.sum` / `package-lock.json` / `Cargo.lock`.

### Changed
- `airlock sentinel probe|check` skips a model whose provider has no probe support instead of aborting the sweep. Skipped ids are listed in the report and counted as `skipped=` in the text output. Previously one such model failed the whole run, reachable now that discovery attributes models to providers Airlock cannot call.
- Docs: in-repo GUIDE, ROADMAP, and blog posts are gone. The README walkthrough stays here. The full guide and roadmap live on [xdlc.dev](https://xdlc.dev/airlock/docs/guide).
- Docs: app repos use `uses: xdlc-labs/airlock@v0`. The in-repo workflow is dogfood (`uses: ./`), not a file to copy. Snapshot ids are described as stable. `airlock ci` always writes the comment file, so `--comment` is not part of the walkthrough.

## [0.1.0-beta.13] – 2026-09-08

Makes `uses: xdlc-labs/airlock@v0` use the release binary. Required if you follow
the README, which now recommends the moving tag.

### Fixed
- Pushing the moving `v0` tag published a release. The Release workflow triggered
  on `v*`, which matches `v0`, so repointing the tag created a release named
  "Airlock 0" with version-less `airlock_0_<os>_<arch>` assets, and GitHub served
  that as the repository's latest release ahead of every real version. The trigger
  now requires a full version tag.
- The Action built its download URL straight from `github.action_ref`, so a
  major-only ref asked for `releases/download/v0/airlock_0_<os>_<arch>.tar.gz`,
  which is not a release name and never existed. Every `@v0` run 404'd and fell
  back to compiling Go from source. A major-only ref now resolves to the release
  it currently points at; an unmatched one still falls back to the source build.

## [0.1.0-beta.12] – 2026-09-08

Fixes a gate that did not fire: widening an MCP server's `permissions:` was not
detected, so `--fail-on-approval` let it through. Pin `uses: xdlc-labs/airlock@v0`
or `@v0.1.0-beta.12`.

### Highlights
- MCP permission expansion is gated again. If you relied on it in an earlier beta, it was not gating.
- The README walkthrough reaches a real `PASS` on a fresh clone.
- An `INCONCLUSIVE` min gate now reports the sample count it would take to resolve.

### Known limits
- Windows install not supported yet
- Comparative gates still report `SKIPPED` until a baseline exists

### Added
- `stats.SamplesToClearMin` reports the smallest sample count whose Wilson lower bound can reach a gate's `min`. An `INCONCLUSIVE` min gate now says how many clean samples it would take to resolve, so an unreachable threshold is visible instead of looking like a flaky run.
- `docs/assets/demo.sh` regenerates the README demo recording from a clone.
- The developer guide and roadmap live in `docs/` again, and `docs/assets/` carries the brand mark, so the README no longer depends on an external site.

### Changed
- Gate reasons wrap at a fixed column instead of stretching their box. A long reason used to push the eval table past 160 columns and break it in an ordinary terminal.
- A `SKIPPED` gate prints `-` for rate and interval rather than `0.0%`, which read as a total failure rather than a gate awaiting a baseline.
- Eval metrics are sorted by name. They came out of a map, so two identical runs could produce different PR comments.
- `airlock ci` reports `NEEDS_APPROVAL` as the headline verdict when a human gate is pending, unless a gate outright failed. `INCONCLUSIVE` outranks `NEEDS_APPROVAL` internally, so the comment used to contradict its own Unblock section. Display only: `--fail-on-eval` and `--fail-on-inconclusive` still read the eval verdict.
- The toy agent ships a committed `.airlock/policy.yml` and a larger sample budget, so the README walkthrough reaches a real `PASS`. The default mins that `init` writes cannot resolve at a three-case sample budget.

### Fixed
- **MCP permission expansion was not gated.** An MCP server's artifact hash was its schema hash alone, and an APM lockfile usually pins that as a literal, so widening `permissions:` did not move it. Diff only inspects artifacts whose hash changed, so a permissions-only edit produced "no AI artifact changes" and sailed past `--fail-on-approval`. Permissions are now part of the artifact hash, sorted so reordering is not a change.
- Box padding counted bytes rather than runes, so any line holding a multi-byte character was over-padded and the right border came out ragged.

## [0.1.0-beta.11] – 2026-09-07

Marketplace display name is `Airlock AI release gate`. Pin `uses: xdlc-labs/airlock@v0.1.0-beta.11`.

### Changed
- `action.yml` `name` is `Airlock AI release gate`.

## [0.1.0-beta.10] – 2026-09-07

Marketplace display name is `Airlock AI gate`. Pin `uses: xdlc-labs/airlock@v0.1.0-beta.10`.

### Changed
- `action.yml` `name` is `Airlock AI gate`. GitHub rejects Marketplace names that match an existing user or org, and [github.com/airlock](https://github.com/airlock) already exists.

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

[Unreleased]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.14...HEAD
[0.1.0-beta.14]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.13...v0.1.0-beta.14
[0.1.0-beta.13]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.12...v0.1.0-beta.13
[0.1.0-beta.12]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.11...v0.1.0-beta.12
[0.1.0-beta.11]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.10...v0.1.0-beta.11
[0.1.0-beta.10]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.9...v0.1.0-beta.10
[0.1.0-beta.9]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.8...v0.1.0-beta.9
[0.1.0-beta.8]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.7...v0.1.0-beta.8
[0.1.0-beta.7]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.6...v0.1.0-beta.7
[0.1.0-beta.6]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.5...v0.1.0-beta.6
[0.1.0-beta.5]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.4...v0.1.0-beta.5
[0.1.0-beta.4]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.3...v0.1.0-beta.4
[0.1.0-beta.3]: https://github.com/xdlc-labs/airlock/compare/v0.1.0-beta.2...v0.1.0-beta.3
[0.1.0-beta.2]: https://github.com/xdlc-labs/airlock/releases/tag/v0.1.0-beta.2
[0.1.0-beta.1]: https://github.com/xdlc-labs/airlock/releases/tag/v0.1.0-beta.1
