---
title: Contributing
---

# Contributing

The repository's
[`CONTRIBUTING.md`](https://github.com/twilight-project/twilight-core/blob/main/CONTRIBUTING.md)
is the full guide (setup, branching, releases, review) and
[`REVIEW.md`](https://github.com/twilight-project/twilight-core/blob/main/REVIEW.md)
describes each CI check. This page is the short version.

## Ground rules for the chain

- **Determinism first.** No wall-clock time, randomness, goroutines, environment
  variables or node-local configuration in any state transition. Sort before iterating a
  map, before writing state, sending funds or emitting events.
- **CoreSlot is the only source of validator updates.** No other module may return
  them.
- **The omitted modules stay omitted.** Staking, distribution, governance, mint and
  slashing are not wired in.
- **`utwlt` only in accounting.** `twlt`/`TWLT` are display metadata and never enter
  amounts.
- **Immutable monetary fields.** `native_denom` and `max_supply` never change after
  genesis.
- **Fail closed.** Finalization runs in a cache context and commits only on full
  success; an unexpected condition returns an error and halts the block rather than
  committing partial state.
- **Never hand-edit generated code.** Regenerate `*.pb.go`, `*.pb.gw.go` and
  `*.pulsar.go` with `make proto`, and the descriptor set under `docs/proto/` with
  `make proto-descriptor`.

## Before opening a PR

`main` accepts changes only through a pull request that passes six required checks (the
one exception is a fix merged from a security advisory's private fork; see `REVIEW.md`):

| CI check | Run locally |
|---|---|
| build & test | `go build ./...` and `go test -count=1 ./...`, plus the first six chain-free checks on [Localnet & Drills](localnet-drills.md#chain-free-checks) |
| consensus vectors | `make consensus-vectors` |
| golangci-lint | `make lint` (CI pins v2.12.2 and fails only on issues new to the PR) |
| gofmt & tidy | `gofmt -l` finds nothing outside `website/` (`make fmt` rewrites the files), and `go mod tidy` leaves `go.mod`/`go.sum` unchanged |
| proto descriptor up to date | `make proto-descriptor` leaves `docs/proto/` unchanged (needs protoc 27.0) |
| govulncheck | `make vuln` |

A change to a consensus or economic path also runs the localnet checks:

```bash
make localnet-smoke
make localnet-rewards-epoch-smoke
make localnet-settlement-smoke
make drills
```

Changes to `x/coreslot`, `x/rewards`, `x/mining`, `app/`, upgrade handlers or genesis
import/export are expected to get a maintainer's review.

## Commits and PRs

- Branch from `main` and open the PR back into `main`; it merges with a merge commit
  (squash and rebase merges are disabled).
- Open an issue first for anything non-trivial, and reference it from the PR.
- Use [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat(rewards): …`, `fix(coreslot): …`, `docs: …`).
- Keep each change to one purpose: economics changes are not bundled with docs or
  wiring changes.
- Report a security vulnerability privately, following
  [`SECURITY.md`](https://github.com/twilight-project/twilight-core/blob/main/SECURITY.md),
  never as a public issue.

## Docs (this site)

The site lives in `website/` and is separate from the Go build:

```bash
cd website && npm ci && npm run build   # a broken link or anchor fails the build
```

The *docs* workflow builds it on every pull request that touches `website/` and
publishes it from `main`.

- Trace every documented behaviour to code, a real command, or a recorded run. Anything
  that is not supported yet goes under a labelled "Known limitations" or "Planned"
  section.
- Make no mainnet or production-ready claims, and keep localnet and test settings
  distinct from production defaults.
- Keep shared numbers in `website/docs/_snippets/constants.mdx`.
- Write command tables from the built binary's `--help` and the pinned query surface
  (`internal/queryapi/contract.go`), and do not commit `--help` captures; they go stale
  silently.
- Say what a key or role controls and how to respond to its loss; do not publish
  step-by-step attack sequences.
