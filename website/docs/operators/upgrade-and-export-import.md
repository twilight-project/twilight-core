---
title: Upgrade & Export/Import
---

# Upgrade & Export/Import

## On-chain upgrades

The chain wires the standard `x/upgrade` module. Only the CoreSlot authority can
schedule or cancel a plan; there is no governance module, and none is needed
(design record: ADR-0003 in the repository's `docs/architecture/adr/`).

```bash
# the authority schedules a plan: a handler name, the halt height, free-form info
twilightd coreslot schedule-upgrade <name> <height> <info> --from <authority-key> --chain-id <chain-id> --node <rpc>
twilightd coreslot cancel-upgrade --from <authority-key> --chain-id <chain-id> --node <rpc>

# anyone can read it
twilightd query upgrade plan --node <rpc>
twilightd query upgrade applied <name> --node <rpc>     # the height a past upgrade ran at
twilightd query upgrade module-versions --node <rpc>
```

### Two kinds of change, two procedures

| | Node-local | State machine |
|---|---|---|
| Examples | pruning, RPC and API settings, indexer, log level, p2p tuning, metrics, hardware | `x/coreslot`, `x/rewards`, `x/mining`, `app/` wiring, parameter structure, proto, the module-account set |
| Release | **patch** (`v0.3.1`) | **minor** (`v0.4.0`), with a registered handler named after the version it upgrades to |
| Procedure | restart one validator at a time | every node halts at the scheduled height, swaps, resumes |
| Downtime | none | seconds, if every operator has staged the binary |

Two nodes running different state-machine logic at the same height compute different
application hashes. That is why the right-hand column cannot roll one node at a time,
and why the left-hand column must never be used for it.

### What happens at the height

1. The plan is committed. From that block until the height, a node that is **already**
   running a binary containing the named handler aborts every block it tries to
   process. Do not swap early.
2. At the height, every node refuses the block: the process stays up and answers RPC,
   but its application height stops. A validator left on the old binary stays stopped
   rather than following the network with the old logic.
3. Each operator starts the new binary. It runs the handler once, in a cache context
   that commits only on complete success, then continues.
4. The chain produces blocks again once more than two-thirds of the voting power is
   back. With four validators, three suffice and the fourth catches up; with fewer, every
   operator must complete the swap before any block is produced.

The settlement clock advances only on blocks that are produced, so a halt freezes every
open settlement window. Nothing expires while the network is down, however long that is.

### Operator procedure

- Run `twilightd` under Cosmovisor, with `DAEMON_ALLOW_DOWNLOAD_BINARIES=false`. A node
  holding balances must never fetch and execute a binary named by an on-chain message.
- Before the height: download the release, verify its SHA-256 against the published
  checksums, and place it where Cosmovisor expects the binary for the plan's name. Do
  not run it.
- After the swap: confirm `query upgrade applied <name>` returns the upgrade height, that
  your node's application hash matches its peers, and that it is signing again.
- `--unsafe-skip-upgrades <height>` makes a node skip a plan. It is a network-wide
  decision: a node that uses it alone computes different state from the rest and forks.

### Scheduling constraints (for the authority)

- **Never schedule an upgrade at an epoch boundary.** The handler runs at the start of
  the upgrade height and epoch finalization at its end; a boundary height puts a
  migration and an epoch close in one block, on the money-critical path. Schedule
  comfortably mid-epoch (on the public testnet an epoch is 360 blocks).
- Pre-stage and hash-verify the binary **before** submitting the plan, and check the
  plan's name against the handler that release ships. A plan naming a handler no binary
  has is recoverable: it is visible in `query upgrade plan`, and the authority can cancel
  it any time before the height.
- For a migration that touches rewards or mining state, consider a rewards pause first.
  It stops accrual and release together and freezes the settlement clock, which gives a
  quiescent state to migrate from.

### Changing block parameters

`block.max_gas` and `block.max_bytes` cannot be changed by any transaction: the consensus
module's authority is a keyless account. On a running network they change only inside a
named upgrade, whose handler sets them (`app.SetBlockParams`), so a new value is scheduled
and halts every validator at the same height, like any other state-machine change. A new
`max_bytes` takes effect from the block after the upgrade height. A new `max_gas` already
applies to the upgrade block's own transactions: one that exceeds it fails in that block. A release that changes `max_bytes` also says what mempool bound
operators should set, since a node's `config.toml` does not follow the change.

### What is proven, and what is not

The mechanism is covered by application tests and by a four-validator localnet drill
with two separately built binaries (`make localnet-upgrade-drill`): every validator halts
at the same height, the upgraded nodes agree on the application hash across the
boundary, a validator left on the old binary fails closed, the settlement clock does not
consume the downtime, and the migration runs exactly once. Three handlers are registered:
`v0.2.0`, `v0.3.0`, and `v0.4.0`, which sets `block.max_gas` to 30,000,000. Before release,
`v0.4.0` was rehearsed from the published `v0.3.1` on four validators and run on a
37-validator devnet under load.

Not yet exercised: store-layout changes (adding, renaming or deleting a store), and
Cosmovisor itself, which the drill swaps by hand so that a tooling failure cannot be
mistaken for a chain one. No upgrade of a running public network is recorded.

One consequence to plan for: a single latest binary cannot replay the chain from genesis
across an upgrade boundary. Recovery and replay use either the historical sequence of
binaries, which Cosmovisor keeps, or a validated snapshot or state sync taken after the
boundary.

## Exporting state

```bash
twilightd export --home <node-home> --output-document state.json
```

This exports the full app state via the module manager's export path, every module in
the same shape as genesis ([Genesis](../reference/genesis-reference.md)).
Beyond what a launch genesis holds, an export of a running chain carries:

- `rewards`: the finalized epoch aggregates, every slot entitlement with its released
  amount, the outstanding entitlement liability, the pause state, the open epoch's
  reward-enabled block count, and any queued params;
- `mining`: every settlement row (open and finalized), the settlement epoch anchors,
  the settlement clock and the materialization cursor;
- `coreslot`: the slots with their lifecycle state, reserved consensus addresses, the
  last-applied validator set, pending key rotations and pending authority transfers.

## An export cannot be re-imported by this binary

The export is **complete**: every monetary fact of the chain — the finalized-epoch
archive, every entitlement, the liability, the supply, the escrow — is in it, and a test
asserts exactly that. But every Twilight module's importer accepts only a **fresh** genesis, and
refuses a document that carries closed-epoch state, naming it. A continuation importer,
one that restarts a chain from an export of a running chain, is deferred, not written.
So an export is a record for review and for a future continuation path, not a restore
procedure; a node recovers from its own data directory and backups, and an upgrade
continues the existing state in place.

What is tested on the genesis path is narrower: a populated genesis document (non-default
state and a queued params update, no closed epoch) round-trips through the app's module
manager byte-for-byte and re-imports into a fresh app with its state preserved.

Any upgrade must preserve the immutable `native_denom` and `max_supply` and the
finalized epoch and entitlement history; a future continuation importer must preserve
them exactly as the export records them.
