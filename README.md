<p align="center">
  <img src="docs/assets/mark.png" width="72" alt="xdlc-labs">
</p>

<p align="center">
  <strong>Airlock</strong>
</p>

<p align="center">
  <strong>Your coding agent widened its own MCP write access in a pull request.<br>Nothing in CI noticed.</strong>
</p>

<p align="center">
  <a href="https://github.com/xdlc-labs/airlock/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/xdlc-labs/airlock/ci.yml?style=flat-square&label=tests" alt="Tests"></a>
  <a href="https://github.com/xdlc-labs/airlock/releases"><img src="https://img.shields.io/github/v/release/xdlc-labs/airlock?include_prereleases&style=flat-square" alt="Release"></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/go-1.25+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square" alt="License"></a>
</p>

<p align="center">
  <img src="docs/assets/demo.gif" width="760" alt="Airlock blocking an MCP permission expansion in CI">
</p>

Airlock diffs AI artifacts on a pull request, runs evals, and then passes, fails, or asks a human. State stays in `.airlock/` in your repo. Nothing uploads.

## Install

Linux and macOS, `amd64` and `arm64`. Pin a pre-release tag. GitHub "latest" skips them.

```bash
curl -sSL https://raw.githubusercontent.com/xdlc-labs/airlock/main/install.sh | AIRLOCK_VERSION=v0.1.0-beta.13 bash
# or: go install github.com/xdlc-labs/airlock/cmd/airlock@v0.1.0-beta.13
```

## Try it

Clone this repository. The toy agent under `testdata/toy-agent` needs no API keys.

```bash
cd testdata/toy-agent
airlock init && airlock snapshot
```

```console
╭─ airlock init ───────────────────────────────╮
│ agents  1     models  1     prompts  2       │
│ tools   0     skills  1     mcp      2       │
│ evals   2                                    │
│                                              │
│ wrote  .airlock/manifest.json                │
╰──────────────────────────────────────────────╯
╭─ snapshot ───────────────────────────────────╮
│ dca3ae3e3db0bd58                             │
│ artifacts  10    manifest  1ed9ab1f53b8      │
╰──────────────────────────────────────────────╯
```

Snapshot before you edit. Reword the system prompt and ask what it touches:

```bash
echo "You are a DIFFERENT support agent." >> prompts/system.md
airlock diff
```

```console
╭─ airlock ───────────────────────────────────────────────────╮
│ base  dca3ae3e3db0bd58                                      │
│ head  working-f78985673552                                  │
│                                                             │
│ changed                                                     │
│   ~  env          toy-env              04c19f51 -> 9fca1701 │
│   ~  prompt       system-prompt        a68e98e0 -> 31125b19 │
│                                                             │
│ blast radius                                                │
│   agents  support-bot                                       │
╰─────────────────────────────────────────────────────────────╯
```

Two artifacts move because `env.json` bundles that prompt. Then run the evals:

```bash
airlock test --mode replay
```

```console
╭─ eval ────────────────────────────────────────────────────────────╮
│ metric               rate             95% CI  gate                │
│ json_valid        100.0%  [ 89.3%, 100.0%]  PASS                  │
│     CI low 0.8928 >= min 0.8000                                   │
│ task_success            -                  -  SKIPPED             │
│     no baseline result to compare against — run `airlock baseline │
│     create` (or pass --base) to enable this gate                  │
│ tool_success      100.0%  [ 80.6%, 100.0%]  PASS                  │
│     CI low 0.8064 >= min 0.8000                                   │
╰───────────────────────────────────────────────────────────────────╯
╭─ verdict ─────────────────────────────────────────────────────╮
│ PASS                                                          │
│ samples=48  cost=$0.0048  wrote  .airlock/results/latest.json │
╰───────────────────────────────────────────────────────────────╯
```

Gates fire on confidence intervals. A comparative gate with no baseline reports `SKIPPED` and does not fail the build on its own.

Widen MCP permissions instead:

```bash
# add write under mcp.local-fs.permissions in apm.lock.yaml
airlock ci --fail-on-approval
```

```console
╭─ airlock ───────────────────────────────────────────────────╮
│ base  dca3ae3e3db0bd58                                      │
│ head  working-71cd85c12f4e                                  │
│                                                             │
│ changed                                                     │
│   ~  mcp          local-fs             e1156c01 -> 05e044b6 │
│                                                             │
│ blast radius                                                │
│   agents  support-bot                                       │
│                                                             │
│   MCP new permission write on local-fs                      │
│   MCP permissions expanded: local-fs                        │
╰─────────────────────────────────────────────────────────────╯
╭─ verdict ───────────────────────────────────────────────────────────╮
│ NEEDS_APPROVAL                                                      │
│ airlock approve --base dca3ae3e3db0bd58 --head working-71cd85c12f4e │
│ wrote  .airlock/ci-comment.md                                       │
╰─────────────────────────────────────────────────────────────────────╯
error: airlock ci: NEEDS_APPROVAL without ledger entry
exit 1
```

Evals can still pass. The gate blocks until a human runs `airlock approve`.

## Use it on your repo

```yaml
# .github/workflows/airlock.yml
name: Airlock
on: pull_request
permissions:
  contents: read
  pull-requests: write
jobs:
  airlock:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: xdlc-labs/airlock@v0
```

`@v0` tracks the newest release. Pin an exact tag if you want to move slower. The Action fail-closes on permission expansion.

`airlock init` writes `.airlock/policy.yml` if it is missing. Default mins are strict. An undecided gate prints how many samples it needs. Lower the min, raise `max_samples_per_case`, or pass `--fail-on-inconclusive`.

## What it gates

Airlock gates AI release risk on the pull request. It is not a general AppSec scanner.

| Airlock blocks | Keep using |
|---|---|
| MCP, write-tool, and skill permission expansion (`--fail-on-approval`) | CodeQL and other SAST |
| Eval regressions and undecided gates (`--fail-on-eval`, `--fail-on-inconclusive`) | Dependabot, Socket, `cargo-vet` |
| PII or secrets in model input and output (`data_boundary.fail_on_pii`) | Repository secret scanning |
| A new dependency landing with a prompt, skill, or MCP change | SCA on dependency-only pull requests |

A lockfile bump alone is Dependabot's job. Airlock speaks up when an AI-artifact change and a new dependency arrive together.

Keep LangSmith, Braintrust, Promptfoo, or Phoenix for traces and datasets. Import cases with `airlock import promptfoo|langsmith|braintrust`. Airlock is the ship-or-block decision on the PR.

## Commands

| Command | Job |
|---|---|
| `init` / `snapshot` | Discover artifacts, freeze a release record |
| `diff` | What changed, which agents it reaches |
| `test` / `ci` | Evals and the PR verdict |
| `approve` / `rollback` | Human gate, re-pin a known-good snapshot |
| `sentinel` | Fingerprint a model behind a stable name |
| `ingest` / `baseline` / `drift` | Production loop from local OTel JSONL |
| `import` / `eval` / `judge` | Bring in cases, promote, calibrate |
| `history --serve` | Local read-only UI |

Discovery covers APM lockfiles, skills, Cursor rules, MCP configs, prompts, Promptfoo, common lockfiles, and OpenAI SDK / LangGraph heuristics. The [guide](https://xdlc.dev/airlock/docs/guide#what-init-discovers) lists what is and is not detected.

[Guide](https://xdlc.dev/airlock/docs/guide) ·
[Roadmap](https://xdlc.dev/airlock/docs/roadmap) ·
[Changelog](CHANGELOG.md) ·
[Contributing](CONTRIBUTING.md) ·
[Security](SECURITY.md) ·
[Apache-2.0](LICENSE)
