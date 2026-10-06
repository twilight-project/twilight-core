---
title: Overview
---

# Getting Started — Overview

Twilight is a minimal Cosmos SDK / CometBFT **Proof-of-Authority** chain:

- **CoreSlot** (`x/coreslot`) owns validator admission, lifecycle state, and
  validator-set updates. Validator membership is not token-staked: there is no
  staking, delegation, or slashing path.
- **Rewards** (`x/rewards`) mints a bounded `utwlt` reward pool each epoch and
  holds finalized rewards as per-slot entitlements until settlement in `x/mining`
  releases them.

If you are new, read in this order:

1. [Install](install.md) — build `twilightd`.
2. [Quickstart](quickstart.md) — run a localnet and query rewards.
3. [Chain concepts](chain-concepts.md) — the mental model.
4. [Glossary](glossary.md) — terminology.

Then dive into the [Rewards overview](../rewards/overview.mdx),
[Chain architecture](../chain/architecture.md), or the
[Operator guides](../operators/node-operator-guide.md).

For current validation evidence and known limitations, see
[Status & Validation](../chain/status-and-validation.md).
