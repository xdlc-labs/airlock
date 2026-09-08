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

---

Your test suite checks that the code still works. It does not check that the agent
still behaves. A prompt gets reworded, a skill lands, an MCP server picks up a
`write` permission, a provider swaps the model behind a stable string, and every
check stays green because no test asserts on any of it.

Airlock is a release gate for that surface. It treats prompts, skills, tools, MCP
servers, models, judges, and eval sets as one releasable unit, diffs what changed,
evaluates behavior against policy with confidence intervals, and then passes,
fails, or holds the pull request for a human.

<p align="center">
  <img src="docs/assets/demo.gif" width="760" alt="Airlock blocking an MCP permission expansion in CI">
</p>

That is the whole point in twelve seconds. The evals pass. The gate still blocks,
because the blast radius includes a new `write` permission on an MCP server, and
that needs a person. Walk through it below on the toy agent.

- **Local-first.** State lives in `.airlock/` in your own repo. Nothing uploads.
- **No API keys to try it.** The toy agent uses a mock provider.
- **Beside your eval platform, not instead of it.** Keep LangSmith or Promptfoo.

## Install

Linux and macOS, `amd64` and `arm64`. Windows is not supported yet.

```bash
curl -sSL https://raw.githubusercontent.com/xdlc-labs/airlock/main/install.sh | AIRLOCK_VERSION=v0.1.0-beta.13 bash
# or: go install github.com/xdlc-labs/airlock/cmd/airlock@v0.1.0-beta.13
```

This is a public beta, so every release is a pre-release and GitHub's "latest"
link skips them. Pin the tag above, or pick one from
[Releases](https://github.com/xdlc-labs/airlock/releases).

## Break a prompt, watch Airlock catch it

Clone this repository. The toy agent in `testdata/toy-agent` ships prompts, a
skill, one MCP server, and eval cases.

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

Now reword the system prompt and ask what it touches:

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

`diff` compares the working tree against the last snapshot, so take the snapshot
*before* you make the change. The toy agent's `env.json` bundles that prompt into
an environment artifact, which is why two artifacts move for one edit.

Then run the evals against the mock provider:

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

Gates fire on confidence intervals, never on a point estimate. A comparative gate
with no baseline yet reports `SKIPPED` rather than inventing a verdict, and it
never fails the build on its own.

Now the interesting one. Widen an MCP server's permissions instead:

```bash
# add "write" under mcp.local-fs.permissions in apm.lock.yaml
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

Adding a skill, widening a write tool, or a live MCP server growing a new tool in
its `tools/list` all take the same path. `airlock approve` records the decision in
a ledger so the next run knows a human said yes.

The full walkthrough, including `--mode live`, judges, drift, and the production
loop, is in the [developer guide](https://xdlc.dev/airlock/docs/guide).

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

That is the whole install. `@v0` tracks the newest release, or pin an exact tag
if you would rather move deliberately. The Action diffs merge-base against HEAD,
writes `.airlock/ci-comment.md`, and comments on the pull request. It fails closed
on permission expansion by default.

`airlock init` writes a `.airlock/policy.yml` stub you can commit and tune. Its
default mins are strict on purpose: a `0.99` gate needs at least 381 clean samples
before a 95% interval can clear it, so if you leave the sample budget low that
gate will report `INCONCLUSIVE` forever. Airlock now tells you the number it
needs. Raise `max_samples_per_case`, lower the min, or add
`--fail-on-inconclusive` so an undecided gate blocks instead of passing quietly.

## What it gates, and what it does not

Airlock gates **AI release risk on the pull request**. It is not a general
application security scanner.

| Airlock blocks | Keep using |
|---|---|
| MCP, write-tool, and skill permission expansion (`--fail-on-approval`) | CodeQL and other SAST |
| Eval regressions and undecided gates (`--fail-on-eval`, `--fail-on-inconclusive`) | Dependabot, Socket, `cargo-vet` |
| PII or secrets appearing in model input and output (`data_boundary.fail_on_pii`) | Repository secret scanning |
| A new dependency riding along with a prompt, skill, or MCP change | SCA on dependency-only pull requests |

That last row is the narrow claim worth being precise about: a dependency bump on
its own is Dependabot's job and Airlock stays quiet. It speaks up when an
AI-artifact change and a new dependency arrive in the same pull request, which is
what an agent proposing its own tools looks like. Details in the
[roadmap](https://xdlc.dev/airlock/docs/roadmap#agent-driven-supply-chain).

## If you already use LangSmith, Braintrust, Langfuse, or Phoenix

Keep them. They trace runs, hold datasets, and let you iterate on prompts in a UI.
Airlock is the ship-or-block decision on the pull request, which none of them make
for you. Point Airlock at eval cases you already trust with
`airlock import promptfoo|langsmith|braintrust`, feed production signal through
`airlock ingest otel`, and leave your traces where they are. There is no native
connector yet, and no hosted dashboard here at all. See the
[roadmap](https://xdlc.dev/airlock/docs/roadmap#langsmith--braintrust--langfuse--phoenix).

## What is in the box

`init` and `snapshot` build a content-addressed record of the AI system. Snapshot
ids stay stable across CI runs: `generated_at` and the absolute root path are not
hashed. `diff` reports what moved and which agents it reaches. `test` and `ci` run
statistical evals and decide. `approve` and `rollback` handle human gates and
re-pinning a known-good release. `sentinel` fingerprints upstream models so you
notice when a provider changes one under a stable name. `ingest otel`, `baseline`,
and `drift` close the loop from production. `history --serve` gives you a
read-only local UI.

Discovery covers APM lockfiles, Agent Skills, Cursor rules, MCP configs, prompt
files, Promptfoo suites, `go.sum`, `package-lock.json`, `pnpm-lock.yaml`,
`yarn.lock`, `Cargo.lock`, `poetry.lock`, `Pipfile.lock`, `uv.lock`, and
heuristics for the OpenAI SDK and LangGraph. It is not every framework yet. The
[guide](https://xdlc.dev/airlock/docs/guide#what-init-discovers-today) lists exactly what is and is not
detected today, and the [roadmap](https://xdlc.dev/airlock/docs/roadmap) covers the rest.

## Status

Public beta. Expect discovery gaps and CLI churn before 1.0. No telemetry, no
hosted control plane, nothing uploads by default.

[Guide](https://xdlc.dev/airlock/docs/guide) ·
[Roadmap](https://xdlc.dev/airlock/docs/roadmap) ·
[Changelog](CHANGELOG.md) ·
[Contributing](CONTRIBUTING.md) ·
[Security](SECURITY.md) ·
[Apache-2.0](LICENSE)
