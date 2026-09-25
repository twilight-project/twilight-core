# Twilight Core

[![CI](https://github.com/twilight-project/twilight-core/actions/workflows/ci.yml/badge.svg)](https://github.com/twilight-project/twilight-core/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/twilight-project/twilight-core?include_prereleases&sort=semver)](https://github.com/twilight-project/twilight-core/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/twilight-project/twilight-core.svg)](https://pkg.go.dev/github.com/twilight-project/twilight-core)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](go.mod)

> [!WARNING]
> **Public testnet, pre-1.0.** Twilight Core has **not been externally audited** and is
> provided "as is", without warranty of any kind (see [LICENSE](LICENSE)). Testnet tokens
> have **no monetary value**. Report newly discovered vulnerabilities privately as described
> in [SECURITY.md](SECURITY.md) — never in a public issue.

Twilight Core is a Cosmos SDK and CometBFT Proof-of-Authority chain. The node binary is
`twilightd`. Full documentation: **https://twilight-project.github.io/twilight-core/**

Validator admission and validator-set updates are owned exclusively by `x/coreslot`: the
chain uses CoreSlot operators rather than a public staking validator set. The standard
staking, distribution, governance, mint, and slashing modules are **intentionally omitted**
(not wired into the app), so none of those flows are exposed to users. The `auth`, `bank`,
and `consensus` modules are present, so standard account and token operations (e.g.
`twilightd tx bank send`) work as usual.

The native base denomination is `utwlt` — the canonical accounting denomination. The
display denomination is `twlt` (symbol `TWLT`, name Twilight) with six decimal places
(1 `twlt` = 10^6 `utwlt`).

## Architecture

**CoreSlot Proof-of-Authority (`x/coreslot`).** The validator set is formed from a capped
set of authority-governed *CoreSlots* (the active-set size is bounded by configurable
min/max parameters). Operators are admitted to a slot and move through a lifecycle
(`register → activate → inactivate / suspend → reactivate → remove`, plus consensus-key
rotation). The module's EndBlocker translates active slots into CometBFT validator-set
updates — and it is the **only** source of `ValidatorUpdate`s in the app. There is no
delegation, staking, slashing, or unbonding.

**Rewards & emission (`x/rewards`).** Block subsidy is emitted on an epoch schedule: at
epoch finalization, `utwlt` is minted into the rewards module account, tracked against a
maximum supply with **supply-threshold halving** (not a fixed block-height schedule). Each
epoch's emission is allocated to eligible active operators by active-block participation and held
as a per-`(slot, epoch)` entitlement until settlement releases it. An emergency authority can
pause rewards, which stops accrual and release together. The chain's **default genesis has no premine**.

**Settlement (`x/mining`).** Value leaves the rewards escrow only through settlement. When
an epoch is finalized, `x/mining` materializes that epoch's settlement set, anchored to its
block-driven settlement clock; each slot's settlement transactions then pay participants by
chunk and finalize by returning the remainder to the payout address snapshotted at
finalization. `x/mining` holds
no bank keeper and no funds — every transfer goes through `x/rewards`.

**Determinism.** Epoch finalization runs in a cache context and is written only on full
success — on any unexpected condition it fails closed (errors without committing partial
state). See the [determinism rules](CONTRIBUTING.md#determinism-rules-important-for-a-chain)
contributors follow.

## Modules

| Module | Responsibility | CLI |
|---|---|---|
| [`x/coreslot`](x/coreslot) | Validator admission, slot lifecycle, consensus-key rotation, payout address, reward weight, and validator-set updates | `twilightd coreslot …`, `twilightd coreslot-query …` |
| [`x/rewards`](x/rewards) | Epoch emission, supply-threshold halving, per-operator reward allocation and entitlements, emergency pause | `twilightd rewards …`, `twilightd rewards-query …` |
| [`x/mining`](x/mining) | Settlement of per-`(slot, epoch)` entitlements: settlement clock, settlement-set materialization, chunked payout and finalization | `twilightd tx mining …`, `twilightd mining-query …` |

Standard `tx`/`query` subcommands for the wired Cosmos modules (`bank`, `auth`, `consensus`)
are generated via AutoCLI — e.g. `twilightd tx bank send`, `twilightd query bank balances`.

## Repository layout

```
app/              application wiring (depinject runtime app, module registration, upgrades)
cmd/twilightd/    node binary entrypoint
x/coreslot/       CoreSlot PoA module (validator admission + set)
x/rewards/        rewards / emission module
x/mining/         settlement module
internal/         shared internal packages (checked arithmetic, address rules, query API)
proto/twilight/   protobuf definitions (coreslot, rewards, mining)
scripts/          release, provenance, smoke, and CI check scripts
scripts/localnet/ localnet bring-up, smoke tests, and chaos drills
docs/             architecture, ADRs, operator guides, proto descriptors, references
website/          Docusaurus documentation site
tools/            auxiliary tooling (the read-only dashboard)
```

## Prerequisites

- **Go 1.25.x** (the version pinned in `go.mod`), **make**, and **git** — to build and test.
- **jq** and **curl** — used by the localnet scripts.
- **protoc** — only if you regenerate protobuf (`make proto`).
- **Node.js + npm** — only to build the documentation site under `website/`.

## Build, test, and run

```bash
make build            # stamped binary at build/twilightd
make test             # go test ./...
make localnet-smoke   # spin up a 4-node localnet, run a sanity pass, tear down
```

Bring up a local network manually, or run the rewards/chaos suites:

```bash
make localnet-init             # initialise a local multi-node genesis
./scripts/localnet/start.sh    # start the nodes  (./scripts/localnet/stop.sh to stop)

make localnet-rewards-epoch-smoke  # rewards epoch finalization + entitlements
make drills                    # lifecycle + restart-rotation + quorum drills
```

The localnet setup funds its authority and emergency-authority accounts for local use only;
the chain's default genesis has no premine.

Release binaries (`make build-release`) are built from `git archive HEAD` and ship with
`SHA256SUMS`, `LICENSE`, `NOTICE`, and `THIRD_PARTY_NOTICES` (the licenses of every
statically linked dependency) — see [CONTRIBUTING.md](CONTRIBUTING.md).

## Network interfaces

A running node exposes the standard Cosmos / CometBFT interfaces:

| Interface | Default port | Use |
|---|---|---|
| CometBFT RPC | 26657 | blocks, `/block_results`, `/tx`, `/validators`, consensus/node info |
| gRPC | 9090 | typed query/tx services (coreslot, rewards, mining, and the enabled Cosmos modules) |
| REST (gRPC-gateway) | 1317 | JSON wrapper over the gRPC query services |

When `api.swagger` is enabled, merged OpenAPI docs are served at `/swagger/` on the REST
port.

## Documentation

The documentation site is **https://twilight-project.github.io/twilight-core/** — start with
[Install](https://twilight-project.github.io/twilight-core/getting-started/install/) and
[Status & Validation](https://twilight-project.github.io/twilight-core/chain/status-and-validation/)
(what has and has not been validated). Its source is under [`website/`](website/).

In this repository: [architecture overview](docs/architecture/overview.md),
[ADRs](docs/architecture/adr/README.md), [CONTRIBUTING.md](CONTRIBUTING.md),
[REVIEW.md](REVIEW.md).

## Contributing

Contributions are welcome. For non-trivial changes, open an issue first to discuss the
approach. Consensus-critical paths (`x/coreslot`, `x/rewards`, `x/mining`, `app/` wiring,
upgrade handlers, and genesis import/export) get extra review — see
[CONTRIBUTING.md](CONTRIBUTING.md) and [REVIEW.md](REVIEW.md).

## Security

**Report newly discovered vulnerabilities privately**, through
[GitHub private vulnerability reporting](https://github.com/twilight-project/twilight-core/security/advisories/new)
— never in a public issue. Known limitations of the public testnet are tracked openly as
issues. See [SECURITY.md](SECURITY.md) for scope and what to expect.

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
Copyright 2026 The Twilight Project Authors — see [NOTICE](NOTICE).
