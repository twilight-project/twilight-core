---
title: Architecture
---

# Chain Architecture

Twilight is a [Cosmos SDK](https://docs.cosmos.network/) application chain running
on CometBFT consensus. It is deliberately minimal: a **Proof-of-Authority (PoA)**
validator model owned by `x/coreslot`, a scheduled-emission rewards module
`x/rewards`, and a settlement module `x/mining` through which reward value leaves
escrow. The standard `staking`, `distribution`, `slashing`, `governance`, and `mint`
modules are **omitted**; `auth`, `bank`, `consensus` and `upgrade` are present.

## Module map

| Module | Role |
|---|---|
| `x/coreslot` | Owns the validator/operator slot set; the **only** module that emits validator updates. Stores each slot's operator, payout and settlement addresses, reward weight, and status. Holds the chain's authority, which also schedules upgrades, and the emergency authority. Exposes read-only interfaces to rewards and mining. |
| `x/rewards` | Reads the active CoreSlot set, counts active blocks per epoch, finalizes epochs, mints `utwlt`, and creates the per-slot entitlements that settlement later releases. Owns the escrow and the only code that moves value out of it. Does **not** manage validators. |
| `x/mining` | Settlement: materializes each finalized epoch's settlement set against a block-driven settlement clock, admits participant payout chunks, and finalizes a settlement by releasing the remainder to the snapshotted payout address. Holds **no** bank keeper; every transfer goes through `x/rewards`. See [Settlement](../rewards/settlement.md). |
| `upgrade` | The standard `x/upgrade` module, reachable only through CoreSlot's `ScheduleUpgrade` / `CancelUpgrade`: the module's own messages are bound to an authority with no key. See [Upgrade & Export/Import](../operators/upgrade-and-export-import.md). |
| `auth`, `bank`, `consensus` | Standard Cosmos SDK modules (accounts, balances/supply, consensus params). |

The runtime is wired via Cosmos SDK `depinject`; the three custom keepers are
constructed manually in `app/app.go`, mining last, since it reads the other two.
Module accounts and the lifecycle order are declared in `app/config.go`; upgrade
handlers are registered in `app/upgrades.go`.

## Consensus and lifecycle interfaces

This is the single most important architectural fact for anyone reasoning about
consensus safety:

- **CoreSlot uses the legacy ABCI EndBlock** interface
  (`module.HasABCIEndBlock`) and is the **sole emitter of validator updates**.
- **Rewards and mining use the modern `appmodule` lifecycle** interfaces
  (`appmodule.HasBeginBlocker` / `HasEndBlocker`), which return **only an error**
  and can never return validator updates.
- Lifecycle order (set in `app/config.go`):

  | Hook | Order |
  |---|---|
  | PreBlock | `upgrade`, `auth` |
  | BeginBlock | `rewards` (CoreSlot and mining have no BeginBlocker) |
  | EndBlock | `coreslot`, `rewards`, `mining` — the validator set is resolved first, then the epoch's accounting, then its settlement set |

The block-by-block sequence, with a diagram, is on
[Block Lifecycle](lifecycle.md).

## Module boundaries

Dependencies run one way, and nothing depends on mining:

- **Rewards reads CoreSlot** through four methods only: `GetActiveSlots`, `GetSlot`,
  `GetAuthority`, and `GetEmergencyAuthority`. It never writes CoreSlot state, never
  reads reward weight, and never reads consensus power for accounting.
- **Mining reads rewards** through a narrow interface — the finalized epoch, its
  entitlements, epoch geometry, the pause state — and calls exactly two methods that
  move value: a participant payout against an entitlement and the release of its
  remainder to the operator. Both are enforced by rewards against the entitlement it
  owns, so a defect in mining cannot widen what leaves escrow.
- **Mining reads CoreSlot** for a slot's record (its settlement address decides who
  may submit chunks) and the active set.
- **CoreSlot knows nothing** of rewards or mining.

Because staking/distribution/slashing/governance are absent, there is no
delegation, no proposer reward, no slashing penalty, and no on-chain governance
proposal flow. The validator authority and the emergency authority are CoreSlot
concepts (see [Consensus & CoreSlot](consensus-and-coreslot.md)); the same
authority is the only account that can schedule an on-chain upgrade.

## Where things live

| Path | Contents |
|---|---|
| `app/` | App wiring (`app.go`), module/account config (`config.go`), the upgrade-handler registry (`upgrades.go`), params (`params/`). |
| `x/coreslot/` | CoreSlot module (PoA validator authority). |
| `x/rewards/` | Rewards module (emission, epochs, entitlements, the release boundary, params, invariants). |
| `x/mining/` | Settlement module (settlement clock, materialization, chunks, finalization, parameter histories). |
| `cmd/twilightd/` | The `twilightd` node + CLI binary. |
| `scripts/localnet/` | Localnet init/start/agree/stop + smoke, soak, and drill scripts. |
