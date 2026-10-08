---
title: Twilight Chain Documentation
slug: /intro
---

# Twilight Chain Documentation

Twilight is a [Cosmos SDK](https://docs.cosmos.network/) / CometBFT
**Proof-of-Authority (PoA)** application chain. Validator admission and validator
updates are owned exclusively by the **CoreSlot** module (`x/coreslot`); the
standard staking, distribution, mint, slashing, and governance modules are omitted.

The chain mints scheduled block rewards through the **rewards** module
(`x/rewards`): each epoch, a `utwlt` reward pool is minted and allocated to the
active CoreSlot operators as per-slot entitlements, which settlement later
releases to participants and to each operator's snapshotted payout address.

**Twilight Testnet** is live: a public, pre-1.0 network that is not externally
audited and whose tokens have no value. This site documents the `main` branch; the
latest release is [v0.3.0](https://github.com/twilight-project/twilight-core/releases),
and a command documented here may be newer than that binary. See
[Status & Validation](chain/status-and-validation.md) for what has been validated
and what has not.

## What `utwlt` is

`utwlt` is the **only** denomination used for on-chain accounting (balances,
minting, rewards, settlement). `twlt` / `TWLT` / "Twilight" are **display metadata
only** (6 decimals) and never appear in accounting state.

## Core capabilities

- **CoreSlot** (`x/coreslot`) — the sole authority over validator admission,
  lifecycle state, consensus keys, and validator-set updates.
- **Rewards** (`x/rewards`) — supply-threshold block emission, active-block
  participation allocation, epoch finalization, carry-forward remainder
  accounting, slot entitlements, queued parameter updates, and emergency
  pause/resume.
- **Settlement** (`x/mining`) — the release of each slot's entitlement: a
  block-driven settlement clock, a settlement set materialized when an epoch
  closes, participant payouts by chunk, and finalization that returns the
  remainder to the snapshotted payout address. Holds no funds of its own.
- **On-chain upgrades** (`x/upgrade`) — scheduled only by the CoreSlot authority;
  a coordinated halt at a height, then the registered handler.
- **Query and transaction surfaces** — CLI, gRPC and REST interfaces for reading
  chain state and submitting the supported transactions.

## Start here

- **New to the chain:** [Install](getting-started/install.md) to build `twilightd`, the
  [Quickstart](getting-started/quickstart.md) to run a localnet and query it,
  [Chain concepts](getting-started/chain-concepts.md) for the mental model, and the
  [Glossary](getting-started/glossary.md).
- **Running a node:** the [Node Operator Guide](operators/node-operator-guide.md), then
  [Monitoring](operators/monitoring.md) and
  [Upgrade & Export/Import](operators/upgrade-and-export-import.md).
- **Running a validator:** [Validator Operations](operators/validator-operations.md) and
  [Keys, Backup & Recovery](operators/keys-backup-and-recovery.md).
- **Building on the chain:** the [Integrator Guide](reference/integrators.md), then
  [CLI & Queries](reference/cli.md) and [Events](reference/events.md).
- **Understanding the design:** [Architecture](chain/architecture.md), the
  [Rewards overview](rewards/overview.mdx) and [Settlement](rewards/settlement.md).
- **Contributing:** [Contributing](development/contributing.md) and
  [Localnet & Drills](development/localnet-drills.md).
