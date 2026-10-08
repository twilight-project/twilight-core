---
title: Testing
---

# Testing

What to run, and which layer covers which risk. The end-to-end targets are on
[Localnet & Drills](localnet-drills.md); the repository's testing records are under
`docs/testing/` (the validation summary, the CoreSlot test plan, the module
simulations and the drill reports).

## Commands

The `make` targets run what CI runs:

```bash
make build                 # stamped binary at build/twilightd (CI: go build ./...)
make test                  # go test ./...  (CI adds -count=1)
make consensus-vectors     # protocol-vector conformance
make lint                  # golangci-lint (CI pins v2.12.2 and gates only new issues)
make vet                   # go vet ./...  (CI: golangci-lint's govet)
make vuln                  # govulncheck (pinned inside the script; blocking in CI)
```

Narrower runs while working:

```bash
go test ./x/coreslot/... -count=1
go test ./x/rewards/... -count=1
go test ./x/mining/... -count=1
go test ./app -count=1                        # wiring, settlement on the real bank, export/import, upgrades
go test ./app -run Simulation -count=1        # the two seeded state-machine simulations
```

## Which layer covers which risk

| Layer | Risk covered |
|---|---|
| `x/coreslot/keeper`, `types` | slot lifecycle and its guards, key rotation, authority nomination and acceptance, upgrade scheduling, validator-update derivation, genesis validation, invariants |
| `x/rewards/keeper`, `types` | emission and halving math, active-block accounting, atomic finalization, allocation, entitlements and the release boundary, pause, params and their histories, invariants |
| `x/mining/keeper`, `types` | the settlement clock, materialization, chunk validation, both finalization arms, parameter histories, the economic-address rule, the Selection V1 contracts |
| `x/*/client/cli` | each command builds the request or message it claims to; the query surface matches the pinned contract in `internal/queryapi` |
| `internal/consensusvectors` | the tracked protocol-vector packs (`make consensus-vectors`) against the functions that implement them |
| `app` | the assembled app: module wiring and lifecycle dispatch, settlement against the real bank, the transfer rules, query classification and height pinning, export/import, upgrade handlers |
| simulations (`app`, `-run Simulation`) | seeded random operation sequences for CoreSlot lifecycle and rewards accounting, with the invariants checked after every step; each seed is fixed, so a failure reproduces |
| [localnet drills](localnet-drills.md) | what needs more than one node: cross-node app-hash agreement, quorum, real signed transactions, coordinated upgrades |

## Some app-level tests

| Test | Covers |
|---|---|
| `TestRewardsRuntimeDispatchFinalizeBlock` | the runtime dispatches rewards BeginBlock/EndBlock; exact supply delta |
| `TestRewardsRuntimeFinalizeBlockFailClosed` | a lifecycle fault halts the block, with no partial commit |
| `TestDefinitivePOC1SettlementEndToEnd` | a full 360-block epoch through settlement and finalization, with exact economics |
| `TestSettlementChunkMovesRealParticipantBalances` | a settlement chunk pays participants on the real bank |
| `TestPermissionlessFinalizationPaysTheOperatorAndNotTheCaller` | permissionless finalization sends the remainder to the slot's payout address, never to the caller |
| `TestRewardsAppGenesisExportImportRoundTrip` | the rewards genesis, pending params included, survives export and re-import through the app's module manager |
| `TestBothUpgradeBoundariesAreRegisteredOnTheBuiltApp` | the built app registers every upgrade handler it ships |

## Determinism expectations

State transitions are integer-only and read no wall-clock time, randomness,
environment variables or node-local configuration; they iterate sorted collections.
The multi-node evidence is cross-node app-hash agreement after the transition under
test, which the smokes, the three `make drills` drills, and the upgrade, export-restore,
block-gas, growth and soak runs check. See [Status & Validation](../chain/status-and-validation.md).
