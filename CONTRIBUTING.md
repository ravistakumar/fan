# Contributing to fan

Thanks for your interest! `fan` is written in Go and uses standard tooling.

## Development

```bash
make build   # build the binary
make test    # run all tests
make lint    # golangci-lint
```

## Conventions

- **Functional core, imperative shell**: keep logic in `task`, `schedule`,
  `result`, `summary`, and `planner` (pure, unit-tested) and I/O in `agent`,
  `vcs`, `gate`, and `run`.
- **New agent CLIs**: add a constructor in `internal/agent` — one case in the
  `New` switch, behind the `Agent` interface. The prompt is always passed as
  the final argument.
- **TDD**: write the failing test first. Every PR runs `go test ./...` and
  lint.
- **No API keys in tests**: integration tests use a stub agent script in a
  `t.TempDir()` and real `git` in a temp repo. No network, no agent cost.

## Submitting changes

1. Fork the repository.
2. Create a branch: `git checkout -b my-change`.
3. Make your changes and add tests.
4. Run `make test` and `make lint` locally.
5. Open a pull request with a clear description of the change and why.

All contributions are subject to the [Code of Conduct](CODE_OF_CONDUCT.md).
