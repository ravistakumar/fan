# fan — agent-agnostic parallel coding-agent orchestrator

**Date:** 2026-06-03
**Status:** Design approved, ready for implementation planning

## Summary

`fan` runs a list of independent coding tasks in parallel, one headless agent
per task, each isolated in its own git worktree. Tasks that apply cleanly and
pass a user-defined gate are auto-merged onto a throwaway integration branch;
everything else is queued for review. The developer's working branch is never
touched.

`fan` is agent-agnostic: it reuses the developer's already-authenticated agent
CLI (no API keys), the same model as its sibling tool `prr`. Where `prr`
sharpens a single prompt before handoff, `fan` runs many prompts in parallel
across any supported agent.

## Motivation

Claude Code shipped Dynamic Workflows (research preview, May–June 2026): a
native swarm that fans work across many subagents with a generator/validator
loop. It is Claude-only. Codex, OpenCode, Aider, and Grok Build have no
equivalent cross-agent fan-out. `fan` fills that gap for the agents the native
feature does not cover, keeping the terminal-first, bring-your-own-auth posture
that `prr` established.

## Scope

### v0.1 workload

Fan out **independent** tasks: a developer supplies (or has a planner draft) a
list of self-contained tasks, and `fan` runs one agent per task concurrently.
Tasks are assumed independent — no ordering or dependency graph in v0.1.

### Non-goals (deferred)

- Decompose-a-feature-then-integrate (planner produces interdependent subtasks
  that must be merged into one coherent change). Candidate for a later release.
- Token/cost tracking and budgets. A different problem; out of scope here.
- Piping each task prompt through `prr` as a refiner. Candidate for v0.2.
- Cross-task dependency ordering. v0.1 tasks are independent by definition.

## Supported agents

Five one-shot, headless CLIs at launch, each invoked through its own
authenticated session (no API keys):

| Agent       | Headless invocation                                    |
| ----------- | ------------------------------------------------------ |
| Claude Code | `claude -p "<prompt>"`                                  |
| Codex       | `codex exec "<prompt>"`                                 |
| OpenCode    | `opencode run --prompt "<prompt>"`                     |
| Aider       | `aider --yes --no-auto-commits --message "<prompt>"`   |
| Grok Build  | `grok --no-auto-update -p "<prompt>"`                  |

Adapters follow the proven `prr` pattern: an `askArgs`/`launchArgs` shape with
the prompt appended last. Adding an agent is one `case`. Each agent's flags are
pinned from its own documentation; any agent not smoke-tested live before
release ships labeled **experimental** in the README (the policy `prr` used for
Aider).

## Architecture

`fan` follows the `prr` split: a pure functional core (planning, scheduling,
merge decisions) wired to an imperative shell (worktrees, agents, gate, git) by
a runner.

```
                ┌─────────────────────────────────────────────┐
   task list ─► │  Planner (optional --plan)  → approved list  │
   or --plan    └─────────────────────────────────────────────┘
                                  │
                                  ▼
                ┌─────────────────────────────────────────────┐
                │  Scheduler  (concurrency cap, e.g. cores-1)  │
                └─────────────────────────────────────────────┘
                       │            │            │
              ┌────────┘     ┌──────┘      └────────┐
              ▼              ▼                       ▼
        ┌──────────┐  ┌──────────┐            ┌──────────┐
        │ worktree │  │ worktree │   ...      │ worktree │
        │  +agent  │  │  +agent  │            │  +agent  │
        └──────────┘  └──────────┘            └──────────┘
              │              │                       │
              ▼              ▼                       ▼
        ┌─────────────────────────────────────────────────┐
        │ Integrator:  clean apply? + gate passes?         │
        │   yes → merge to integration branch (1 commit)   │
        │   no  → queue for review                         │
        └─────────────────────────────────────────────────┘
                                  │
                                  ▼
                    summary table + integration branch
```

### Functional core (pure, unit-tested)

- Planner-JSON → task-list parsing and validation.
- Scheduler concurrency math.
- Merge-decision state machine (the four terminal states below).
- Status → summary mapping.
- `{}` template substitution for `--each`.

### Imperative shell

- **Worktree**: create/teardown one `git worktree` per task.
- **Agent**: the five adapters, invoked headless.
- **Gate**: run a user-defined check command inside a worktree.
- **Git**: cherry-pick, conflict detection, revert, branch management.

## Input

Two surfaces, matching the "explicit core + planner mode" decision.

### Explicit (reliable core)

Inline template form — `{}` is substituted per item:

```bash
fan run --each 'src/ui/*.tsx' --agent codex --gate 'npm test' \
    'Migrate {} from the old Button API to the new one'
```

Task-file form (TOML, like `prr`'s config) for heterogeneous work:

```toml
# tasks.toml
[defaults]
agent       = "codex"        # claude | codex | opencode | aider | grok
gate        = "npm test"     # empty = "clean apply only"
concurrency = 6              # default: cores-1

[[task]]
id     = "migrate-button"
prompt = "Migrate src/ui/Button.tsx to the new API"
agent  = "claude"            # per-task override

[[task]]
id     = "migrate-modal"
prompt = "Migrate src/ui/Modal.tsx to the new API"
```

Run with `fan run tasks.toml`.

### Planner mode (convenience)

```bash
fan plan --agent claude 'Migrate every component in src/ui to the new Button API'
```

One agent acts as **planner**: it explores the repo headless and emits a
structured task list against a JSON schema `fan` enforces. `fan` renders the
list as a preview table and — confidence-gated, like `prr`'s clarifying step —
asks the developer to approve, edit, or drop items before anything runs. The
approved plan can be written to a file with `--save tasks.toml`, turning a
one-off into a repeatable, hand-editable artifact.

```
 #  id              agent   prompt                                  gate
 1  migrate-button  codex   Migrate src/ui/Button.tsx to new API    npm test
 2  migrate-modal   codex   Migrate src/ui/Modal.tsx to new API     npm test
 3  migrate-tabs    codex   Migrate src/ui/Tabs.tsx to new API      npm test
   [a]pprove all  [e]dit  [d]rop #  [c]ancel
```

## Execution and integration

### Per-task lifecycle

Each task runs in the worker pool, up to `concurrency` at once:

1. **worktree** — `git worktree add .fan/wt/<id> <integration-base>` (detached
   off HEAD).
2. **agent** — run the agent headless in that worktree with the task prompt.
3. **capture** — `git add -A && commit` the result as one commit,
   `fan(<id>): <prompt summary>`. No file changes → status `no-op`, skip.
4. **gate** — run the gate command **inside the worktree** (isolated). Pass →
   candidate for merge. Fail → queue (`gate-fail`).
5. **integrate** — cherry-pick the commit onto the integration branch,
   serialized one task at a time so merges cannot race. Clean → `merged`.
   Conflict → queue (`conflict`); the integration branch is left untouched.
6. **teardown** — keep the branch ref; remove the worktree directory.

### Two safety decisions

- **Gate runs in the isolated worktree, before merge.** A task proves itself on
  its own before it is allowed near the integration branch. After all merges,
  `fan` runs the gate **once more on the integration branch** to catch
  interaction bugs between independently-passing tasks (the classic swarm
  failure). If that final run is red, the integration branch is flagged — but it
  is only a branch, so it can be inspected or discarded.
- **Merges are serialized and one-commit-each, onto a throwaway branch.**
  Auto-merge is N sequential cherry-picks onto `fan/<timestamp>`, each trivially
  `git revert`-able. The developer's working branch is never checked out or
  modified. This is the guarantee that makes auto-merge safe.

### Terminal states

| Status             | Meaning                              | Developer action                         |
| ------------------ | ------------------------------------ | ---------------------------------------- |
| `merged`           | clean apply + gate green             | none — it is on the integration branch   |
| `queued: conflict` | gate green, cherry-pick conflicted   | resolve from its kept branch             |
| `queued: gate-fail`| applied, but gate failed             | inspect its worktree branch              |
| `error`            | agent crashed / timed out / no change| `fan retry <id>`                         |

### Output

A summary table plus an explicit next step:

```
fan: 7 tasks · 5 merged · 1 conflict · 1 gate-fail · integration branch: fan/2026-06-03-1432
  ✓ merged       migrate-button, migrate-modal, migrate-tabs, migrate-card, migrate-input
  ⚠ conflict     migrate-menu      → branch fan/migrate-menu
  ✗ gate-fail    migrate-tooltip   → branch fan/migrate-tooltip (npm test failed)
  final gate on integration branch: PASS

  review:  git diff main..fan/2026-06-03-1432
  ship:    git merge fan/2026-06-03-1432      (the developer decides; fan never does)
```

`fan` auto-merges into the integration branch but never into the real branch.
The last mile — merging `fan/...` into `main` — is always the developer's call.

## Stack

Deliberately identical to `prr`:

- **Go**, single static binary.
- `cobra` (CLI), `BurntSushi/toml` (task files), `charmbracelet/huh` (the
  approve/edit prompt), `charmbracelet/bubbletea` + `lipgloss` (live parallel
  progress view — the one new ingredient over `prr`, which is single-stream).
- Git operations shell out to `git` (worktree, cherry-pick, revert); no libgit2
  dependency.
- **GoReleaser + GitHub Actions**; Homebrew **cask** via the existing
  `ravistakumar/homebrew-tap`.

## Testing strategy

The functional-core/shell split keeps most logic pure and cheap to test.

### Functional core (unit, no I/O)

Planner-JSON parsing and validation, scheduler concurrency math, the
merge-decision state machine, status → summary mapping, and `{}` template
substitution.

### Imperative shell (integration, with fakes)

- **Fake agent**: a stub binary the adapter is pointed at. It writes a
  deterministic file, or exits non-zero, or hangs — exercising the merge,
  `gate-fail`, `error`, and timeout paths. The whole fan-out runs hermetically,
  with no network and no real agent cost.
- **Real git, temp repos**: a throwaway repo in `t.TempDir()` with actual
  `git worktree` / `cherry-pick`, testing clean-merge, conflict, and gate-fail
  paths against real git behavior.
- **Gate**: tested with trivial commands (`true` / `false`) and a real check.

### Live smoke test (manual, pre-release)

One real 2-task fan-out against each of the five agents, the bar `prr` held. Any
agent not smoke-tested live ships labeled **experimental** in the README.

## Open questions for the implementation plan

- Exact `.fan/` working-directory layout and cleanup-on-interrupt behavior.
- Timeout policy per agent run (default, override, per-task).
- Whether the final integration-branch gate is on by default or opt-in.
- `fan retry` semantics (re-run in place vs. fresh worktree).
