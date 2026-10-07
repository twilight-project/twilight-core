---
title: Security & Failure Modes
---

# Security & Failure Modes

## Invariants

The module defines six callable invariants (`x/rewards/keeper/invariants.go`).
They are callable backstops; the chain does not run the `crisis` module, so they
are exercised in tests rather than auto-run on-chain.

| Invariant | Checks | A failure implies |
|---|---|---|
| `SupplyCapInvariant` | bank `utwlt` supply ≤ `max_supply` | over-mint / cap breach |
| `CumulativeEmittedInvariant` | `cumulative_emitted` ≤ `max_supply` | accounting drift past the cap |
| `ModuleBalanceCoverageInvariant` | rewards balance ≥ outstanding entitlements + carry | the module cannot cover what it owes |
| `EntitlementLiabilityInvariant` | the O(1) liability accumulator equals the full scan of entitlement records | the accumulator drifted from what it summarizes |
| `DenomCorrectnessInvariant` | native/fee denom = `utwlt`; no display metadata in amounts | a display denom leaked into accounting |
| `ClosedEpochImmutabilityInvariant` | finalized aggregates embed no per-slot rows; no entitlement releases past its bound | a closed epoch was mutated |

After each finalization and each release, module-balance coverage is expected to
hold: `balance ≥ outstanding entitlements + carry`. These invariants are also
checked in app and localnet validation against real bank state.

## Fail-closed lifecycle

Rewards `BeginBlock` and `EndBlock` are **fail-closed**. A CoreSlot contract
violation, a missing reward snapshot, a cap breach, or any finalization fault
returns an error rather than degrading silently. Under the runtime,
`baseapp.FinalizeBlock` surfaces the error and **discards the block's state**, so
the fault **halts the block rather than half-committing**.

This is the intended safety posture for a monetary module: a halt is recoverable
(the emergency authority can pause rewards to stop value moving while operators
patch), but a half-committed or silently-wrong mint is not. App-level validation
covers this behavior: a forced finalization fault makes `FinalizeBlock` return an
error and leaves the committed height unchanged with no finalized epoch written.

Be precise about what pausing buys you, because it is **not** a way to stop the
epoch lifecycle:

| While paused | Behavior |
|---|---|
| Epoch finalization | **still runs.** The boundary is unconditional — a paused epoch closes, having counted zero reward-enabled blocks, and so emits nothing |
| Settlement materialization | **still runs.** The settlements for whichever epoch just closed are created as usual |
| Settlement clock | **frozen.** A paused chain produces blocks but no settlement time, and paused blocks are not repaid on resume — so settlement deadlines are held, not consumed |
| Monetary release | **stopped.** Chunk submission and settlement finalization are both refused |

The clock freeze is the operationally important half: it means a pause does not
silently burn a settlement's remaining window while an incident is being worked.

:::warning Operator implication
Because rewards is fail-closed, a genuine finalization fault stops block
production until resolved. Keep the CoreSlot active-set contract and rewards
lifecycle aligned (a removed/suspended slot that earned credit is safe — CoreSlot
retains its data). Monitor for repeated EndBlock errors in node logs.
:::

## Determinism

No rewards state transition reads wall-clock time, randomness, environment
variables, or CometBFT-local config. Finalization and release iterate sorted
collections (never raw Go map order). Cross-node app-hash agreement after
finalization is the multi-node evidence.

## Failure-mode reference

| Symptom | Likely cause | Check |
|---|---|---|
| Epoch not finalizing | boundary not reached | `epoch-info` (`current_epoch_end_height`, height) |
| Mint is zero at finalization | rewards paused, or subsidy floored to 0 near cap | `pause-state`; `next-halving` |
| Release rejected | rewards paused, epoch not finalized, no entitlement, or the amount exceeds what remains | `pause-state`; `epoch-reward`; `module-balances` |
| Params update rejected | a field that no transaction may change, an unsupported feature, or the wrong signer | see [Parameters](params.md#what-update-params-rejects) |
| App-hash divergence across nodes | **critical** — a state fork | stop, investigate before continuing; see [Localnet](../chain/localnet.md) |
| Pagination returns empty `next_key` | last page reached | normal; stop paging |

## Authority model

The chain has two governing accounts, both held in CoreSlot params
(`coreslot-query params`), plus the operators of its slots. What each can do is the
whole of the chain's control surface; there is no governance module.

**The authority** (`authority`):

- admits validators: registers, activates, inactivates and removes slots, and requests
  consensus-key rotations — it decides who is in the validator set;
- suspends a slot;
- sets the CoreSlot parameters (set size, delays, lockouts), except the two authority
  fields and, while any slot is active, the voting power;
- **schedules and cancels on-chain upgrades** — the only path to `x/upgrade`, so it
  decides when the chain halts and which handler runs when validators restart on the
  binary the plan names;
- nominates its own successor;
- queues a rewards params update, which today can change two inert fields
  ([Parameters](params.md#rewards)).

**The emergency authority** (`emergency_authority`): pauses and resumes rewards
(effective at the next block; accrual and release stop together), suspends a slot, and
nominates, or cancels a nomination for, its own successor. Nothing else.

**A slot's operator**: its own payout address, settlement address, metadata and
selection policy (refused once the slot is suspended or removed), and self-inactivation
as long as the active set stays at or above `min_active_slots` afterwards. The slot's
settlement address signs participant payouts against the slot's own entitlements
([Settlement](settlement.md)), and the operator decides who holds that address.

**Nobody**, by any transaction: mints outside epoch finalization, moves escrowed value
outside settlement, redirects a payout away from the address snapshotted when the epoch
closed, or changes the denom, the supply cap, the subsidy, the treasury share or the
epoch length. Those live in genesis or in versioned histories that only an upgrade
handler could extend.

### What each key controls, and what to do if it is lost

Custody each key for what it controls, not for how it is usually used.

- **Authority key:** the validator set, the CoreSlot parameters, and on-chain upgrades.
  Because it chooses the validator set, it also decides who earns future emission; it
  cannot touch value already escrowed. If it is compromised: while you still hold the
  role, nominate a fresh key you control and have it accept, ideally in the same block
  (the attacker holds the same power until then); cancel any upgrade plan you did not
  schedule; review the slot set and the parameters.
- **Emergency key:** the pause state and slot suspension (never the last active slot).
  It cannot move value. If compromised: rotate the role the same way, resume, and
  reactivate what it suspended.
- **A slot's settlement credential:** participant payouts against that slot's open
  settlements, up to their full entitlement and nothing beyond
  ([Settlement](settlement.md#chunks)). If compromised: the operator installs a new
  settlement address — not possible once the slot is suspended or removed, so do not
  suspend it first — or the chain is paused.
- **A slot's operator key:** that slot's payout address, settlement address, metadata
  and policy, and nothing of any other slot. Because it installs the settlement
  credential and names the payout address, it carries the same exposure as that
  credential for the slot's open and future entitlements, and must be custodied
  accordingly. If compromised: pause to stop payouts, and have the authority suspend
  or remove the slot so it earns no further entitlements; a settlement already open
  stays payable by whoever holds the settlement address until its deadline passes,
  after which anyone may finalize it to the payout address snapshotted when its epoch
  closed.
