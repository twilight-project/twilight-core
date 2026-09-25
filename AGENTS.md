# AGENTS.md

Instructions for AI coding agents (and a quick orientation for humans) working in
`twilight-core`. This is the canonical agent entry point; `CLAUDE.md` points here.
Read this before making changes.

## What this is

Twilight Core is a greenfield **Cosmos SDK / CometBFT Proof-of-Authority chain**. The
node binary is `twilightd`. Two custom modules carry the design:

- **`x/coreslot`** — validator admission and the validator set (PoA). Operators are
  admitted to authority-governed *CoreSlots*; its EndBlocker is the **only** source of
  CometBFT `ValidatorUpdate`s in the app. No delegation, staking, slashing, or unbonding.
- **`x/rewards`** — epoch emission of `utwlt` with supply-threshold halving, per-operator
  allocation by active-block participation, and per-`(slot, epoch)` entitlements released by
  settlement. An emergency authority can pause rewards, which stops accrual and release
  together.

The standard **staking, distribution, governance, mint, and slashing** modules are
**intentionally omitted** (not wired into `app/`). `auth`, `bank`, and `consensus` are
present, so normal account/token ops (`twilightd tx bank send`) work.

Denominations: base **`utwlt`** (accounting), display **`twlt`** (`TWLT`, 6 decimals;
1 `twlt` = 10^6 `utwlt`).

## Setup & verify

Requires **Go 1.25.x** (see `go.mod`). Run these before pushing — they run the same
checks CI enforces (the `make` targets are the local shorthand; CI is the source of
truth and differs in a few ways noted inline):

```bash
make build     # stamped binary at build/twilightd  (CI builds all packages: go build ./...)
make test      # go test ./...
make fmt       # gofmt
make lint      # golangci-lint             (CI pins v2.12.2, gates only-new-issues)
make vet       # go vet ./...
make vuln      # govulncheck               (CI pins @v1.5.0 and is blocking)
make tidy      # go mod tidy must leave go.mod/go.sum clean
```

For exact CI-parity build/test locally, run `go build ./...` and `go test ./...`
directly; the pinned tool versions live in `.github/workflows/ci.yml`.

Consensus/economic changes must also pass the local end-to-end checks:

```bash
make localnet-smoke           # 4-node localnet sanity
make localnet-rewards-epoch-smoke  # rewards epoch finalization + entitlements
make localnet-settlement-smoke     # settlement money-movement proof (real payouts)
make drills                   # lifecycle + restart-rotation + quorum chaos drills
```

CI (`.github/workflows/ci.yml`) runs six checks, all **required to merge** by the `main`
branch ruleset: build & test, **consensus vectors**, `golangci-lint` (only-new-issues),
`gofmt` + clean `go mod tidy`, an up-to-date **proto-descriptor** check, and a **blocking
`govulncheck`**. All tool versions are pinned deliberately. The ruleset also requires a pull
request, allows **merge commits only**, and blocks force-push and deletion of `main`.

## Hard invariants — do not break

These are consensus- and value-critical. A change that violates one is wrong even if it
compiles and tests pass:

1. **`x/coreslot` is the only source of `ValidatorUpdate`s.** Nothing else may emit them.
2. **Intentionally-omitted modules stay omitted** — do not wire in staking, distribution,
   gov, mint, or slashing.
3. **Immutable rewards params:** `native_denom` and `max_supply` can never change.
4. **Fail-closed determinism.** Rewards `BeginBlock`/`EndBlock` and epoch finalization run
   in a cache context and commit only on full success; on any unexpected condition they
   return an error (halt the block) rather than committing partial/silently-wrong state.
   No state transition may read wall-clock time, randomness, env vars, or node-local
   config, and must iterate sorted collections (never raw Go map order).
   See [`REVIEW.md`](REVIEW.md) and [`docs/architecture/adr/`](docs/architecture/adr/).
5. **`utwlt` is the only accounting denom** — no display denom (`twlt`/`TWLT`) may leak
   into amounts.

Consensus-critical paths (`x/coreslot` set/lifecycle, `x/rewards` finalization/emission,
`app/` wiring, upgrade handlers, genesis import/export, anything affecting deterministic
state or `ValidatorUpdate`s) are expected to get a maintainer review — see
[`REVIEW.md`](REVIEW.md). That is a process expectation, not a ruleset gate: with a single
maintainer, required approvals are zero; code-owner routing and required approvals will be
added once there is more than one maintainer.

## Do not hand-edit generated code

Regenerate instead of editing:

- `*.pb.go`, `*.pb.gw.go`, `*.pulsar.go` → `make proto`
- `docs/proto/` descriptor set → `make proto-descriptor` (byte-reproducible; pinned protoc)

## Workflow & conventions

- **Issue-first** for anything non-trivial; open a focused PR that references it
  (`Closes #N`). Check `gh issue list` before filing to avoid duplicates.
- **Security issues:** report a **newly discovered vulnerability** privately through GitHub
  Private Vulnerability Reporting, never as a public issue — follow
  [`SECURITY.md`](SECURITY.md). Known testnet limitations are the exception: they are
  tracked openly as GitHub issues (testnet tokens have no value), and opening or updating
  such tracking issues is allowed and expected.
- **Commits** are authored under the contributor's own git identity (no AI co-author
  trailers). Keep changes small and single-purpose; the bar for `x/coreslot`, `x/rewards`,
  and `app/` is high.
- Fill in the [PR template](.github/PULL_REQUEST_TEMPLATE.md) checklist (tests,
  determinism, fail-closed posture, validator-update provenance, security, migration/docs).

## Where to look

- [`README.md`](README.md) — architecture, module table, repo layout.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — setup and contribution flow.
- [`REVIEW.md`](REVIEW.md) — review process, what the `main` ruleset enforces, and
  determinism rules.
- [`SECURITY.md`](SECURITY.md) — private vulnerability reporting and the public testnet
  policy.
- [`MAINTAINERS.md`](MAINTAINERS.md) — who maintains the project.
- [`docs/architecture/adr/`](docs/architecture/adr/) — design decisions (CoreSlot PoA,
  rewards emission).
- [`docs/testing/`](docs/testing/) — validation summary, drills, simulations.
- [`website/`](website/) — full documentation site (concepts, module reference, operators).
