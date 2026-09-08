# Developer guide

Airlock is a **release gate for AI agents**. It treats models, prompts, tools, skills, MCP servers, judges, and evals as one releasable unit. Then it diffs the change, runs evals, and ships, blocks, or asks for approval.

It is a local-first Go CLI. State lives under `.airlock/` in your **application** repo. Nothing uploads by default.

This is a public beta. Expect discovery gaps and CLI churn before 1.0. See [CHANGELOG](../CHANGELOG.md) for the current tag.

Ordinary CI asks: did the code build, and did tests pass? Airlock asks: did this **AI change** stay within policy, with statistical confidence?

## Who it is for

**A good fit if you**

- Ship an LLM app or agent with prompts, tools, or MCP
- Change prompts or models often and want a PR gate
- Already have eval cases (or Promptfoo)
- Can export OTel GenAI-style spans for baselines later

**A weak fit if you**

- Run a CRUD service with no model, prompt, or tool surface
- Need a hosted org dashboard today (there is no hosted control plane)

Typical repos: a support bot, a RAG assistant, a tool-using agent, an MCP workflow, a multi-agent tree with APM lockfiles.

## How it fits your stack

Airlock sits **beside** frameworks, gateways, and observability. It does not replace them.

| You already use… | Airlock’s job |
|------------------|---------------|
| LangGraph / LangChain / Vercel AI SDK / a custom agent | Release gate in that **git repo** |
| **LangSmith** / Braintrust (traces, datasets, online evals, playground) | Keep them to observe and iterate. Airlock gates **ship / block / approve** on the PR — [roadmap](ROADMAP.md#langsmith--braintrust--langfuse--phoenix) |
| Langfuse / Datadog / Phoenix (OTel) | `ingest otel` → baseline and drift |
| LiteLLM / Bifrost / Portkey | Airlock emits routing hints. The gateway is the actuator |
| Microsoft APM | Import `apm.lock.yaml`. Airlock does not re-implement package resolution |
| Promptfoo | `import promptfoo` → Airlock eval JSONL |

There is no first-party `integrate litellm|langgraph|langsmith` plugin yet. Integration is files, OTel JSONL, and routing JSON. Native connectors come later (Phase 5+).

## How a change is gated

```text
edit prompt / skill / model / MCP / tools
        ↓
airlock snapshot          # freeze what the AI system is
        ↓
airlock diff              # what changed, and which agents it hits
        ↓
airlock test / airlock ci # evals + policy verdict
        ↓
ship / block / human approve
```

## Quick path (toy agent)

From the Airlock repository, after install or `go build -o airlock ./cmd/airlock`:

```bash
cd testdata/toy-agent
airlock init && airlock snapshot
# edit a file under prompts/
airlock snapshot && airlock diff
airlock test --mode replay
airlock ci --comment
```

## Install

Pin a pre-release tag from [Releases](https://github.com/xdlc-labs/airlock/releases), or copy the command from [README — Install](../README.md#install). Every release is still a pre-release, so GitHub “latest” skips them all and you have to name a tag.

```bash
curl -sSL https://raw.githubusercontent.com/xdlc-labs/airlock/main/install.sh | AIRLOCK_VERSION=<tag> bash
# or: go install github.com/xdlc-labs/airlock/cmd/airlock@<tag>   # Go 1.25+
```

The Action is the exception. `uses: xdlc-labs/airlock@v0` follows a moving major tag, so a workflow picks up each release without being edited, and the Action resolves that tag to the release binary rather than compiling from source. Pin an exact tag there instead if you would rather move deliberately.

Maintainers cutting releases: [RELEASING.md](RELEASING.md).

## Day-to-day commands

Run these in your **agent / app** repo (`--path DIR` is optional).

### Inventory

```bash
airlock init          # discover artifacts → .airlock/manifest + policy stub
airlock snapshot      # content-addressed release snapshot
airlock diff          # vs previous snapshot: changes + affected agents
airlock history       # local release history (optional --serve :8787)
```

### Eval and CI

```bash
airlock test --mode replay          # cassette HTTP when possible (cheap)
airlock test --mode live            # real provider calls
airlock test --affected             # only cases for agents in the blast radius
airlock test --adversarial          # injection / jailbreak-style suite

airlock import promptfoo promptfoo.yaml

airlock ci --comment                # markdown for PR bodies
airlock ci --fail-on-eval
airlock ci --fail-on-inconclusive   # also fail when a gate cannot resolve PASS/FAIL
airlock ci --fail-on-approval       # block until approve on permission / skill expansion
```

Copy [`.github/workflows/airlock.yml`](https://github.com/xdlc-labs/airlock/blob/main/.github/workflows/airlock.yml) into the **application** repo, not the Airlock source repo. The sample defaults to fail-on-approval (`AIRLOCK_FAIL_ON_APPROVAL`, default `true`).

### Security in CI

Airlock is an **AI change-control** gate. It is not a replacement for AppSec scanners.

- **Does:** MCP / write-tool / skill `NEEDS_APPROVAL`, adversarial suites on MCP and skill diffs, eval gates, optional PII fail on model I/O.
- **Does not:** CodeQL, dependency CVEs, whole-repo secret scanning. Keep those jobs.

Company default: `--fail-on-approval`, and usually `--fail-on-eval`. Approvals are advisory until that flag is set.

`--fail-on-eval` only trips on a `FAIL` verdict. With default thresholds (`0.99` / `0.995` min) and `max_samples_per_case: 5`, a metric’s confidence interval can straddle the min forever — stuck `INCONCLUSIVE`, never `PASS` or `FAIL`, and CI stays green. Add `--fail-on-inconclusive` to fail closed on that too. Raise `max_samples_per_case` or widen the gate if you want it to resolve instead of sitting there.

The gate tells you which. A Wilson lower bound for a flawless run is `n / (n + z²)`, so clearing a min takes at least `min · z² / (1 − min)` samples: 16 at `0.80`, 73 at `0.95`, **381** at `0.99`, 765 at `0.995`. An `INCONCLUSIVE` min gate prints the figure alongside what it actually had, so an unreachable threshold reads as unreachable instead of looking like a flaky run. [Why a 99% gate can never pass](blog/your-99-percent-eval-gate-can-never-pass.md) works through it.

### Approvals and rollback

```bash
airlock approve --base <snap> --head <snap>
airlock rollback --to <good-snapshot-id>   # re-pin + routing_decision.json for gateways
```

### Production loop

```bash
airlock ingest otel --file spans.jsonl --redact pii
airlock baseline create --from ingest
airlock drift
```

### Judges

```bash
airlock judge calibrate
airlock judge attribution
```

## Policy knobs

Edit `.airlock/policy.yml` after `init`. `init` writes the stub only when that file is missing, so a policy you commit survives later runs — which is how `testdata/toy-agent` ships gates at a `0.80` min that its three eval cases can actually clear. Useful fields:

- Gates with confidence intervals (`tool_success`, `json_valid`, `task_success`, `adversarial_critical`)
- Budgets (`max_cost_per_pr`, `max_samples_per_case`)
- `fail_on_ai_change`
- `data_boundary.fail_on_pii` — fail if PII or secret patterns appear in model I/O

Gates fire only when a CI **excludes** the threshold. There are no silent point-estimate fails.

MCP or skill artifact changes in `ci` auto-prefer adversarial / injection cases when those exist.

## MCP approval demo

This is the company case: MCP permission expansion must hit a human gate.

From `testdata/toy-agent`, after `airlock init && airlock snapshot`:

1. Widen MCP permissions in `apm.lock.yaml` (for example add `write` under `local-fs.permissions`). For an HTTP(S) server, the live `tools/list` fetch diffs tool names directly, so a new tool on the server fires even when nobody maintains `permissions:`.
2. `airlock snapshot && airlock diff` — expect `NEEDS_APPROVAL` / MCP permission (or new-tool) reasons.
3. `airlock ci --comment --fail-on-approval` — non-zero exit until approved.
4. `airlock approve --base <base-snap> --head <head-snap>`, then re-run `ci` (or merge after the ledger records approval).

Optional: `airlock test --adversarial`. `ci` with MCP or a skill touched auto-prefers injection cases when the suite exists.

Skill adds and edits also raise `NEEDS_APPROVAL` (same fail-closed path).

## What `init` discovers today

`airlock init` is not every industry SDK. What works now:

| Source | Today |
|--------|--------|
| APM (`apm.lock.yaml` / `apm.yml`) | Yes — skills are first-class `skill` (not folded into `tool`) |
| Agent Skills (`SKILL.md` under `.claude/skills`, `.agents/skills`, `.gemini/skills`) | Yes |
| Cursor rules (`.cursor/rules/*.mdc`, `*.md`) | Yes — hashed as `prompt` with source `cursor-rules` |
| MCP configs (`mcp.json`, Cursor / VS Code / Claude Desktop paths) | Yes |
| Prompt files under `prompts/`, `*.prompt.md`, and similar | Yes |
| Model strings in common config / `.env.example` | Heuristic only |
| Promptfoo / eval path globs | Yes (plus LangSmith / Braintrust import) |
| `env.json` | Yes |
| OpenAI SDK + LangGraph (Python / TypeScript / Go heuristics) | Yes |
| OpenAI / Anthropic / Google SDK full AST | Partial; deepening over time |
| Vercel AI SDK, CrewAI, and similar | Not yet |
| Langfuse / remote prompt registries | Not yet |
| Retrieval index / embedding version | Not yet |
| Live MCP schema fetch | HTTP(S) at scan time; stdio is config-hash only |
| `go.sum` / `package-lock.json` / `Cargo.lock` | Yes (supply-chain deps) |

Anything discoverable but not hashable should show up as an **unpinned risk** in the snapshot when we can detect it.

**Package managers (npm / pip):** Airlock does not re-scan every LLM library lockfile. Agent dependency locking is APM’s job. Airlock imports it.

## Solo developer vs company

| | Solo / small team | Company |
|--|-------------------|---------|
| Where | CLI in the agent repo | Same, plus a CI workflow in **app** repos |
| Loop | init → snapshot → diff → test | Plus `ci` on PRs, approvals, policy |
| Prod | optional ingest / drift | baselines from redacted OTel |
| Fail closed | optional | `--fail-on-eval` / `--fail-on-inconclusive` / `--fail-on-approval` |

## What not to expect yet

There is no hosted control plane, enterprise SSO, or autonomic release agent. See the [roadmap](ROADMAP.md). Airlock does not replace Promptfoo, LangSmith, Dependabot / Socket, APM, or a managed agent runtime. It is not a unit-test selection product for ordinary app CI.

**In the OSS beta:** harness skills and rules discovery, first-class `skill`, skill / MCP approval gates, Model Sentinel, eval flexibility, lockfile supply-chain gate. See [discovery](#what-init-discovers-today) and [MCP approval demo](#mcp-approval-demo).

Release notes: [CHANGELOG.md](../CHANGELOG.md).

## Next

- [README](../README.md) — product overview
- [Roadmap](ROADMAP.md) — phases, integrations, non-goals
- [RELEASING.md](RELEASING.md) — cutting GitHub releases
- [CONTRIBUTING.md](https://github.com/xdlc-labs/airlock/blob/main/CONTRIBUTING.md) — developing Airlock itself
