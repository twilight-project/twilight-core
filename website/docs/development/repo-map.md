---
title: Repo Map
---

# Repo Map

Top-level layout of `twilight-core`.

| Path | Contents |
|---|---|
| `app/` | App wiring (`app.go`), module and module-account config (`config.go`), the upgrade-handler registry (`upgrades.go`), the transfer rules (`sendrestriction.go`, `antehandler.go`), the OpenAPI spec (`openapi/`), params bounds (`params/`). |
| `x/coreslot/` | CoreSlot PoA module: slots and their lifecycle, the validator set, key rotation, the two authorities and their rotation, settlement and payout addresses, upgrade scheduling. |
| `x/rewards/` | Rewards module: emission, epochs, allocation, entitlements, the release boundary, pause, params and their histories, invariants. |
| `x/mining/` | Settlement module: the settlement clock, materialization, chunks, finalization, parameter histories, and the Selection V1 contracts (`types/selectionv1`). |
| `internal/` | Shared packages: overflow-checked arithmetic (`checked`), the economic-address rule (`economicaddress`), the pinned query contract (`queryapi`), protocol-vector packs (`consensusvectors`), and source-derived ledgers of error codes and payout accounts. |
| `cmd/twilightd/` | The `twilightd` binary and root command (`cmd/twilightd/cmd/root.go`). |
| `proto/` | Protobuf definitions (`twilight.coreslot.v1`, `twilight.rewards.v1`, `twilight.mining.v1`). |
| `scripts/localnet/` | The localnet harness (`init`, `start`, `stop`, `agree`, `lib/`) and every smoke, drill and soak; see [Localnet Drills](localnet-drills.md). |
| `scripts/` | Proto generation, the descriptor export, release builds, the genesis verifier, the vulnerability scan, consensus vectors and the API smokes. |
| `tools/dashboard/` | A small read-only web dashboard that decodes CoreSlot and rewards state over CometBFT RPC; a separate binary from `twilightd` (`go build ./tools/dashboard`). |
| `docs/` | Architecture decisions (`architecture/adr/`), operator, security and testing records, and the descriptor set for offline transaction decoding (`proto/`). |
| `website/` | This documentation site (separate from the Go build). |
| `Makefile` | Build, test, lint, proto and every localnet target; [Localnet Drills](localnet-drills.md) lists them. |

## `x/rewards/` internals

| Path | Contents |
|---|---|
| `keeper/emission.go` | Pure halving/subsidy/emission math. |
| `keeper/beginblock.go` / `epoch.go` | Active-block crediting; epoch-boundary helpers. |
| `keeper/endblock.go` / `finalize.go` | EndBlock gate; atomic finalization. |
| `keeper/distribution.go` | Active-block participation allocation. |
| `keeper/release.go` | The constrained entitlement-release boundary. |
| `keeper/msg_server.go` / `pause.go` | Msg handlers (update-params, pause, resume) and the pause state. |
| `keeper/query_server.go` | Read-only query server. |
| `keeper/invariants.go` | The six invariants. |
| `keeper/coreslot_reader.go` | The read-only CoreSlot snapshot adapter. |
| `types/` | Proto-generated types, `defaults.go`, `validation.go`, `events.go`, `keys.go`. |
| `client/cli/` | `query.go`, `tx.go` and their tests, including the pinned-contract test. |
| `module.go` | App-module wrapper (modern `appmodule` lifecycle). |

## `x/mining/` internals

| Path | Contents |
|---|---|
| `keeper/endblock.go` | EndBlock: ticks the settlement clock, materializes the settlement set of an epoch that just closed, then promotes the configuration scheduled for the next epoch. |
| `keeper/materialize.go` | The settlement clock, epoch anchoring and materialization. |
| `keeper/chunk.go` | Participant chunk admission. |
| `keeper/finalize.go` | Finalization: the terminal OPEN to FINALIZED transition. |
| `keeper/mode.go`, `settlementparams.go`, `history.go`, `versionlookup.go` | The distribution-mode, selection and settlement-parameter histories, and exact version lookup. |
| `keeper/msg_server.go` / `query_server.go` | Msg handlers and the read-only query server. |
| `types/selectionv1/` | The frozen byte and arithmetic contracts of Participant Selection V1. |

See [Module Map](module-map.md) for the lifecycle/dependency view.
