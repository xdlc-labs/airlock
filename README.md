<p align="center">
  <strong>Airlock</strong>
</p>

<p align="center">
  <strong>AI release engineering. CI gate for prompts, skills, MCP, and models.</strong>
</p>

<p align="center">
  <a href="https://github.com/xdlc-labs/airlock/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/xdlc-labs/airlock/ci.yml?style=flat-square&label=tests" alt="Tests"></a>
  <a href="https://github.com/xdlc-labs/airlock/releases"><img src="https://img.shields.io/github/v/release/xdlc-labs/airlock?include_prereleases&style=flat-square" alt="Release"></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/go-1.25+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square" alt="License"></a>
</p>

---

GitHub Actions for safely shipping AI agents — CI release gate for prompts, skills, MCP, and models. Not another eval platform.

Airlock treats models, prompts, tools, skills, MCP servers, judges, and eval sets as a **releasable unit**, detects what changed, evaluates behavior against policy with **statistical confidence**, and **blocks or approves** the ship — including when the change came from upstream.

The open-source distribution is a local-first Go toolchain (binary + CI Action + `.airlock/` store). Prove that gate in CI first; the hosted control plane comes later (open-core).

> First public beta · no telemetry · [Apache-2.0](LICENSE) · state under `.airlock/` · versions in [CHANGELOG](CHANGELOG.md)

## What you get

- **AI manifest** — agents, models, prompts, tools, skills, MCP, judges, evals
- **Snapshot + behavioral diff** — blast radius with statistical CIs, not point estimates
- **Policy engine** — `PASS` / `FAIL` / `NEEDS_APPROVAL` on the PR
- **Cassette replay** — cheap CI without live provider calls
- **Local-first** — state under `.airlock/`; nothing uploads by default
- **Beside eval platforms** — keep LangSmith / Promptfoo; Airlock is the ship-or-block gate

## The problem

Traditional software has a release pipeline. AI systems change behavior **without** a conventional code change: a provider updates a model behind a stable string, a prompt edits, a skill lands, a tool schema widens, an MCP server gains permissions, a retrieval index drifts, a judge shifts.

The production question is: **can we safely release this AI change?**  
And its mirror: **did a change we never made just get released to us?**

## How it works

```mermaid
flowchart LR
  edit["Edit prompt / skill / MCP"]
  snap["Snapshot"]
  diffNode["Diff + blast radius"]
  policy["Policy: PASS / FAIL / NEEDS_APPROVAL"]
  ship["Ship or block in CI"]
  edit --> snap --> diffNode --> policy --> ship
```

State lives under **`.airlock/`** in your **application** repo. Nothing uploads by default.

## Who it’s for

| Good fit | Weak fit today |
|----------|----------------|
| LLM apps / agents with prompts, tools, skills, or MCP | Pure CRUD with no model/prompt/tool surface |
| Teams that change prompts or models often and want PR gates | Expecting a hosted org dashboard today (Phase 5+) |
| Repos with eval cases / Promptfoo, or OTel GenAI spans | Need full SDK AST for every framework *now* |

**Not yet:** hosted control plane (Phase 5), enterprise SSO / EU / K8s (Phase 6), release agent (Phase 7). See [docs/ROADMAP.md](https://xdlc-labs.github.io/documentation/airlock/roadmap/).

---

## Install

**Platforms:** Linux / macOS · `amd64` / `arm64` (Windows not yet).  
Pin a **pre-release** tag from [Releases](https://github.com/xdlc-labs/airlock/releases) (GitHub “latest” skips them). Current tag: see [CHANGELOG](CHANGELOG.md).

```bash
curl -sSL https://raw.githubusercontent.com/xdlc-labs/airlock/main/install.sh | AIRLOCK_VERSION=v0.1.0-beta.6 bash
# or: go install github.com/xdlc-labs/airlock/cmd/airlock@v0.1.0-beta.6
```

## Quick start - break a prompt, watch Airlock catch it

Clone this repo. Toy agent at `testdata/toy-agent` ships prompts, a skill, MCP, and replay cassettes - **no API keys**.

```bash
cd testdata/toy-agent
airlock init && airlock snapshot
```

```console
$ airlock init && airlock snapshot
Wrote .airlock/manifest.json
  agents=1 models=1 prompts=2 tools=0 skills=1 mcp=2 evals=5
snapshot abc123…  artifacts=12  manifest=def456…
```

Nudge the system prompt (or edit a skill / widen MCP permissions):

```bash
echo "You are a DIFFERENT support agent." >> prompts/system.md
airlock snapshot && airlock diff
```

```console
$ airlock snapshot && airlock diff
snapshot ghi789…  artifacts=12  manifest=jkl012…

Changed AI artifacts:
  ~ prompt:system-prompt (a1b2c3d4 → e5f6a7b8)

Blast radius - agents: support-bot
Blast radius - evals:  default
```

Run cheap replay evals and emit the PR body:

```bash
airlock test --mode replay
airlock ci --comment
```

```console
$ airlock test --mode replay
Verdict: PASS
metric             rate             95% CI  gate
task_success      100.0%  [ 47.8%, 100.0%]  PASS
samples=9 cost=$0.0000  (cassette replay)

$ airlock ci --comment
```

```markdown
### Airlock
This PR changes AI artifacts:
- `changed` **prompt:system-prompt**

Blast radius: agents **support-bot**

### Airlock eval

**Verdict: PASS**

| metric | rate | 95% CI | gate | reason |
|---|---:|---|---|---|
| `task_success` | 100.0% | [47.8%, 100.0%] | **PASS** | no significant regression (delta CI [-0.02, 0.02]) |
```

Flip the story - expand MCP power instead of a prompt:

```bash
# e.g. add "write" under local-fs permissions in apm.lock.yaml
airlock snapshot && airlock diff
airlock ci --comment --fail-on-approval
```

```console
$ airlock diff
Changed AI artifacts:
  ~ mcp:local-fs (… → …)
NEEDS_APPROVAL: MCP new permission write on local-fs

$ airlock ci --comment --fail-on-approval
### Airlock
…
**NEEDS_APPROVAL:** MCP new permission write on local-fs

Run `airlock approve --base … --head …` to unblock.
exit 1   # merge blocked until approved
```

Flip it again - a prompt edit that quietly rides in with a new dependency (agent-driven supply chain):

```bash
# edit prompts/system.md AND add a package under `packages:` in apm.lock.yaml
airlock diff --base <baseline-snapshot-id>
airlock ci --base <baseline-snapshot-id> --fail-on-approval
```

```console
$ airlock diff --base aaee8c11172c86b1
Changed AI artifacts:
  + dependency:left-pad
  ~ prompt:system-prompt (a68e98e0 → 31125b19)
Blast radius - agents: support-bot
NEEDS_APPROVAL: new dependency: left-pad

$ airlock ci --base aaee8c11172c86b1 --fail-on-approval
…
airlock ci: NEEDS_APPROVAL without ledger entry (run: airlock approve --base … --head …)
exit 1   # merge blocked until: airlock approve --base … --head …
```

A dependency added **on its own** (no prompt/skill/MCP/agent change alongside it) does not trigger this - that PR is Dependabot / SCA's job, not Airlock's. Details: [docs/ROADMAP.md](https://xdlc-labs.github.io/documentation/airlock/roadmap/#agent-driven-supply-chain).

That is the company wedge: **AI change control on the PR**, not “hope the prompt looks fine.”

`--mode live` hits real providers (API keys + `budgets.max_cost_per_pr`). Full walkthrough: **[Developer guide](https://xdlc-labs.github.io/documentation/airlock/guide/)**.

## Use it on your repo

1. In your **application** repo: `airlock init` → commit `.airlock/policy.yml` (and keep snapshots as you prefer).
2. Copy [`.github/workflows/airlock.yml`](.github/workflows/airlock.yml) into that repo (not into this one).
3. Company default: fail closed with `--fail-on-approval` / `--fail-on-eval` (sample workflow defaults `AIRLOCK_FAIL_ON_APPROVAL=true`).

### Security in CI

Airlock gates **AI release risk** on the PR - not general AppSec:

| Airlock blocks (with flags) | Still use elsewhere |
|-----------------------------|---------------------|
| MCP / write-tool / **skill** expansion (`--fail-on-approval`) | CodeQL / SAST |
| Adversarial / injection cases when MCP or skills change | Dependabot / SCA / Socket / cargo-vet |
| Eval regressions (`--fail-on-eval`); PII in model I/O | Repo secret scanners |

Skill / MCP power expansion → `NEEDS_APPROVAL`. Approvals are advisory until CI uses `--fail-on-approval`.

**Supply chain (npm, crates.io, PyPI, …):** classic malware-in-the-lockfile is still Dependabot / SCA / provenance. Agents make it worse by proposing or merging deps at machine speed. Airlock’s angle is the **AI release surface**: when a prompt/skill/MCP/agent change also expands an APM-tracked package dependency, that lands in blast radius as `NEEDS_APPROVAL` (`--fail-on-approval` blocks merge) - a dep-only PR with no AI-artifact change is left to SCA. Not replacing package-manager security scanners. Details: [docs/ROADMAP.md](https://xdlc-labs.github.io/documentation/airlock/roadmap/#agent-driven-supply-chain).

### If you use LangSmith (or Braintrust / Langfuse / Phoenix)

Keep the observability + eval platform. Airlock is the **release gate beside it**, not a replacement.

| They do | Airlock does |
|---------|----------------|
| Trace runs, online evals, datasets, playground, annotation queues | Snapshot / diff / policy / CI ship-or-block on the PR |
| Iterate prompts and compare experiments in a UI | Fail closed when prompts, skills, MCP, or models change |

**Today (beta):**

1. Keep tracing and datasets in LangSmith (or similar).
2. In the **application** repo: `airlock init`, point evals at cases you already trust (Promptfoo YAML, or export dataset → Airlock eval JSONL / `airlock import promptfoo`).
3. Tune `.airlock/policy.yml` thresholds; add the [sample workflow](.github/workflows/airlock.yml) with `--fail-on-eval` / `--fail-on-approval`.
4. Optional: feed production signal via `airlock ingest otel` → `baseline` / `drift` (OTel JSONL; not a live LangSmith API sync yet).

**Not yet:** native LangSmith connector, prompt playground, hosted annotation queues, managed agent deploy. Those stay on their platform; Airlock borrows the *flexibility* into later phases without becoming the trace UI. Details: [docs/ROADMAP.md](https://xdlc-labs.github.io/documentation/airlock/roadmap/#langsmith--braintrust--langfuse--phoenix).

---

## What ships in the stack

A full **AI release stack**, not a single command:

| Primitive | Role |
|-----------|------|
| **AI Manifest** | Normalized graph of agents, models, prompts, tools, skills, MCP, judges, evals (imports [APM](https://github.com/microsoft/apm) lockfiles) |
| **Release Snapshot** | Content-addressed record of everything needed to reproduce behavior |
| **Behavioral Diff** | What changed + blast radius; statistical candidate vs baseline (CIs, not point estimates) |
| **Policy Engine** | Gates → `PASS` / `FAIL` / `INCONCLUSIVE` / `NEEDS_APPROVAL` (comparative gates show `SKIPPED` with no baseline yet — never fails closed on its own) |
| **Cassette Store** | Deterministic replay of provider/tool HTTP for cheap CI |
| **Judge Registry** | Pinned, versioned, calibrated evaluators |
| **Production-derived evals** | OTel ingest + local redaction → baselines |
| **Drift detection** | Live vs approved baseline even with no deploy |
| **Data boundary** | Fail release when PII/secrets appear in model I/O (`data_boundary.fail_on_pii`) |
| **Rollback / routing hints** | Re-pin known-good manifest; emit decisions for gateways |
| **Agent-driven supply chain** | APM dependency tracked as blast radius; `NEEDS_APPROVAL` when AI-artifact change co-occurs with a new dependency |
| **Model Sentinel** | Fingerprint upstream models; catch silent provider drift (`airlock sentinel`) |
| **Stack scanner** | OpenAI SDK + LangGraph heuristics; live MCP schema fetch for HTTP servers |
| **Eval flexibility** | Artifact→suite bindings, experiment compare, eval promote, LangSmith/Braintrust import |
| **Lockfile deps** | `go.sum` / `package-lock.json` / `Cargo.lock` → supply-chain blast radius |
| **Control plane** *(Phase 5)* | Team: shared history, approvals, audit, environments, Slack/Teams, regression analytics |
| **Platform** *(Phase 6)* | Enterprise: SSO/RBAC, EU / self-host, K8s admission, publish gate beside SCA |
| **Release agent** *(Phase 7)* | After gate trusted: investigate regressions, recommend rollback, open PRs |

```mermaid
flowchart TB
  inputs["git + APM + prompts + skills + MCP + OTel"]
  engine["Airlock release engine"]
  out["CI decision + gateway routing hints"]
  inputs --> engine --> out
  subgraph parts [Inside the engine]
    discovery["discovery / manifest / snapshot / diff"]
    evals["evals + stats + cassettes + judges"]
    pol["policy → ship / block / approve"]
    store["local .airlock store"]
  end
  engine --- discovery
  engine --- evals
  engine --- pol
  engine --- store
```

## What `init` can see today

`airlock init` is **not** every industry SDK:

| Source | Today |
|--------|--------|
| APM (`apm.lock.yaml` / `apm.yml`) | Yes (skills → first-class `skill`) |
| Agent Skills (`SKILL.md` under `.claude/skills`, `.agents/skills`, `.gemini/skills`) | Yes |
| Cursor rules (`.cursor/rules/*.mdc`, `*.md`) | Yes (as `prompt`, source `cursor-rules`) |
| MCP configs / prompt files / `env.json` | Yes |
| Model strings in config / `.env.example` | Heuristic |
| `go.sum` / `package-lock.json` / `Cargo.lock` | Yes (supply-chain dependencies) |
| Promptfoo / eval globs | Yes (+ LangSmith / Braintrust import) |
| OpenAI SDK + LangGraph (py/ts/go heuristics) | Yes |
| OpenAI / Anthropic / Google SDK full AST | Partial heuristics; deepen over time |
| Vercel AI SDK, CrewAI, … | Not yet |
| Langfuse / remote prompt registries | Not yet |
| Live MCP schema fetch | HTTP(S) servers at scan time; stdio config-hash only |

Agent dependency locking is [APM](https://github.com/microsoft/apm)’s job; Airlock imports it. Details: [GUIDE - discovery](https://xdlc-labs.github.io/documentation/airlock/guide/#discovery-coverage-honest).

## Commands

| Command | Capability |
|---------|------------|
| `init` / `snapshot` / `diff` | Manifest discovery, release snapshots, blast-radius diff |
| `test` / `ci` | Statistical evals + PR release decision |
| `ci --fail-on-eval` / `--fail-on-inconclusive` / `--fail-on-approval` | Fail closed (company default) |
| `import promptfoo\|langsmith\|braintrust` | Bring existing eval corpora |
| `eval promote --from ingest\|results` | Promote failed runs → eval cases |
| `ingest otel` / `baseline create` / `drift` | Production loop |
| `judge calibrate` / `attribution` | Judge as a versioned dependency |
| `approve` / `rollback` | Permission expansion + known-good re-pin |
| `sentinel probe\|check` | Model fingerprint + silent drift detection |
| `history` | Local release history (`--serve` for read-only UI) |

Gates fire only when a confidence interval **excludes** the threshold. Cassettes replay identical provider calls by request hash (not Docker layers).

---

## Status & roadmap

| Phase | Status | Scope |
|-------|--------|--------|
| **0–4** | **Done (OSS beta)** | Release gate: manifest → diff → eval → policy → CI Action (+ Sentinel, stack scan, eval flexibility) |
| **Next** | Proof | 10–20 teams with Action on real agent PRs - harden gate, don’t rush SaaS |
| **5** | Later | Team control plane (open-core paid) |
| **6** | Later | Enterprise platform (SSO, EU, admission, publish gate) |
| **7** | Later | Release agent (investigate / rollback / PR) - only after gate trusted |

```text
OSS release gate (now)  →  prove in CI  →  Phase 5 team plane  →  Phase 6 enterprise  →  Phase 7 release agent
```

**Details:** [docs/ROADMAP.md](https://xdlc-labs.github.io/documentation/airlock/roadmap/) - thesis, open-core, integration maps, explicit non-goals (incl. “not code test selection”).

Design-partner outreach continues (process, not a phase). Release notes: [CHANGELOG.md](CHANGELOG.md).

## Development

```bash
go test ./... -count=1 -race
golangci-lint run ./...
```

This repository’s CI is [`.github/workflows/ci.yml`](.github/workflows/ci.yml) (Go test/lint). Releases: [docs/RELEASING.md](docs/RELEASING.md).

[Contributing](CONTRIBUTING.md) · [Security](SECURITY.md) · [Support](SUPPORT.md) · [Changelog](CHANGELOG.md) · [Guide](https://xdlc-labs.github.io/documentation/airlock/guide/) · [Roadmap](https://xdlc-labs.github.io/documentation/airlock/roadmap/)



## In this org

- [xdlc-agent](https://github.com/xdlc-labs/xdlc-agent) — self-hosted CI Fix daemon
- [documentation](https://xdlc-labs.github.io/documentation/) — hosted guides
- [example-service](https://github.com/xdlc-labs/example-service) — xdlc-agent battleground

## License

[Apache License 2.0](LICENSE)
