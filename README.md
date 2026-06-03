# fan

`fan` fans a list of independent coding tasks across agent CLIs in parallel,
isolates each task in its own git worktree, runs a gate command before touching
any shared state, and auto-merges the tasks that pass cleanly onto a throwaway
integration branch. Your working branch is never modified.

`fan` is agent-agnostic: it drives your already-authenticated agent CLI — no
API keys. It is the parallel counterpart to its sibling tool
[`prr`](https://github.com/ravistakumar/prr), which refines a single prompt
before handoff. Where `prr` sharpens one prompt, `fan` runs many in parallel.

[![CI](https://github.com/ravistakumar/fan/actions/workflows/ci.yml/badge.svg)](https://github.com/ravistakumar/fan/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ravistakumar/fan)](https://github.com/ravistakumar/fan/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/ravistakumar/fan)](https://goreportcard.com/report/github.com/ravistakumar/fan)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

## Install

```bash
# Homebrew (macOS/Linux)
brew install ravistakumar/tap/fan

# Go
go install github.com/ravistakumar/fan/cmd/fan@latest
```

## Quickstart

### Inline template form

Use `{}` as the placeholder. `fan` expands it per item and runs the agent on
each in parallel:

```bash
fan run --each 'src/*.tsx' --agent codex --gate 'npm test' \
    'Migrate {} to the new API'
```

### Task file form

For heterogeneous work, describe tasks in a TOML file:

```toml
# tasks.toml
[defaults]
agent       = "codex"    # claude | codex | opencode | aider | grok
gate        = "npm test" # empty = clean-apply-only
concurrency = 6          # default: cores-1

[[task]]
id     = "migrate-button"
prompt = "Migrate src/ui/Button.tsx to the new API"
agent  = "claude"        # per-task override

[[task]]
id     = "migrate-modal"
prompt = "Migrate src/ui/Modal.tsx to the new API"
```

Run it with:

```bash
fan run tasks.toml
```

## fan plan

Have an agent explore the repository and draft the task list for you:

```bash
fan plan --agent claude --gate 'npm test' \
    'Migrate every component in src/ui to the new Button API'
```

`fan plan` runs one planning pass, presents the proposed task list in a
preview table, and asks you to approve, edit, or drop items before anything
runs. Once you approve, `fan` executes the tasks as it would for a regular
`fan run`. Pass `--save tasks.toml` to write the approved plan to disk as a
repeatable, hand-editable artifact.

```
 #  id              agent   prompt                                  gate
 1  migrate-button  codex   Migrate src/ui/Button.tsx to new API    npm test
 2  migrate-modal   codex   Migrate src/ui/Modal.tsx to new API     npm test
 3  migrate-tabs    codex   Migrate src/ui/Tabs.tsx to new API      npm test
   [a]pprove all  [e]dit  [d]rop #  [c]ancel
```

## Supported agents

| Agent       | Headless invocation                                    | Notes           |
|-------------|--------------------------------------------------------|-----------------|
| Claude Code | `claude -p "<prompt>"`                                 |                 |
| Codex       | `codex exec "<prompt>"`                                |                 |
| OpenCode    | `opencode run --prompt "<prompt>"`                     |                 |
| Aider       | `aider --yes --no-auto-commits --message "<prompt>"`   | EXPERIMENTAL    |
| Grok Build  | `grok --no-auto-update -p "<prompt>"`                  |                 |

Each agent is invoked through its own authenticated session — no API keys
needed. To add an agent, implement one case in `internal/agent`.

Agents marked **EXPERIMENTAL** have not been smoke-tested in a live end-to-end
run against the full pipeline. Use with caution and file an issue with results.

## Safety model

- **Each task runs in its own git worktree**, checked out at the same base
  commit as your current HEAD. Tasks cannot see each other's uncommitted
  changes.

- **The gate runs in the worktree before merge.** A task must pass the gate
  command on its own isolated copy of the code before it is considered for
  integration.

- **Clean and gated tasks are auto-merged onto a throwaway integration branch**
  named `fan/<timestamp>`. Integration is a sequence of cherry-picks, one per
  task, serialized so merges cannot race. Each cherry-pick is independently
  revertable.

- **Conflicts and gate failures are queued, not merged.** A conflicting task is
  preserved as a `fan/<id>` branch for you to inspect and resolve. A gate
  failure is preserved the same way with the gate output attached.

- **Your working branch is never touched.** `fan` creates worktrees off your
  current HEAD and merges only onto its own throwaway branch. Merging
  `fan/<timestamp>` into your real branch is your decision.

- **A final gate runs on the integration branch.** After all tasks complete,
  `fan` runs the gate once more on the combined result to catch interaction bugs
  between independently-passing tasks. If it fails, the integration branch is
  flagged but retained for inspection.

## Task statuses

| Status              | Meaning                                   | What to do                           |
|---------------------|-------------------------------------------|--------------------------------------|
| `merged`            | Clean apply + gate passed                 | Nothing — it is on the integration branch |
| `queued: conflict`  | Gate passed but cherry-pick conflicted    | Resolve from its `fan/<id>` branch   |
| `queued: gate-fail` | Applied, but gate command failed          | Inspect its `fan/<id>` branch        |
| `error`             | Agent crashed, timed out, or made no change | Retry with `fan run --only <id>`   |

## Example output

```
fan: 7 tasks · 5 merged · 1 conflict · 1 gate-fail · integration branch: fan/2026-06-03-1432
  ✓ merged       migrate-button, migrate-modal, migrate-tabs, migrate-card, migrate-input
  ⚠ conflict     migrate-menu      → branch fan/migrate-menu
  ✗ gate-fail    migrate-tooltip   → branch fan/migrate-tooltip (npm test failed)
  final gate on integration branch: PASS

  review:  git diff HEAD..fan/2026-06-03-1432
  ship:    git merge fan/2026-06-03-1432
```

## Contributing

Contributions welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). `fan` is
written in Go with a functional-core / imperative-shell design that keeps logic
easy to test.

## License

[MIT](LICENSE)
