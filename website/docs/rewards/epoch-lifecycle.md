---
title: Epoch Lifecycle
---

# Epoch Lifecycle

An epoch is a window of `epoch_length_blocks` blocks. Active blocks accumulate
during the window; at its end the epoch finalizes.

## Boundary

The configured end height of the open epoch is:

```text
current_epoch_end_height = current_epoch_start_height + epoch_length_blocks − 1
```

The length comes from the epoch-configuration history that governs the epoch, fixed
when the epoch opened. `epoch_length_blocks` in the parameters is frozen genesis
data that no transaction changes, so a parameter update never moves a boundary.

Query it:

```bash
twilightd rewards-query epoch-info --node <rpc>
# state.current_epoch, state.current_epoch_start_height, current_epoch_end_height
```

## Finalization steps

At EndBlock, when the height reaches the boundary, the module finalizes the epoch
atomically (in a cache context, so any fault rolls back entirely). The boundary is
unconditional: a pause does not defer it, a paused epoch simply closes with only
the blocks before the pause counted.

1. Compute the clipped epoch emission ([economics](economics.mdx)). Emission counts
   only reward-enabled blocks, so a fully paused epoch emits zero and cumulative
   emitted does not advance.
2. Assert `cumulative_emitted + emission ≤ max_supply` **before** minting.
3. Mint the (positive) emission as `utwlt` into the `rewards` account.
4. Send the configured treasury share of the emission to the treasury address (zero
   by default, and then nothing is sent), and build the pool:
   `emission + carry_in + fees − treasury` (fees 0).
5. Read the epoch's active-block rows and allocate uniformly by active blocks.
6. Write the immutable epoch aggregate and one slot entitlement per eligible slot.
7. Set `carry_forward_remainder = carryOut`; update `cumulative_emitted`.
8. Delete the closed epoch's active-block rows.
9. Activate pending params, if any, carrying the current enable flags over, and
   clear the queue.
10. Promote any reward configuration scheduled for the next epoch.

The epoch counter does **not** advance here: the next epoch becomes current at its
own first BeginBlock, with `current_epoch_start_height = end + 1`. At the closing
height, `epoch-info` therefore still reports `current_epoch = N` while
`epoch-reward N` already exists. In the same block, `x/mining` materializes the
closed epoch's settlement set — see [Settlement](settlement.md).

## Pause interactions

Rewards has one canonical pause state, set by
[`pause` / `resume`](transactions.md#pause--resume) and effective at H+1.

| While paused | Effect on the epoch |
|---|---|
| Accrual | paused blocks are not reward-enabled, so they credit nothing |
| Emission | a fully paused epoch counts zero reward-enabled blocks and emits nothing |
| Epoch time | unaffected — numbering advances and epochs still finalize |
| Release | entitlement value cannot leave escrow |

## Pending params activation

A params update queued via [`update-params`](transactions.md#update-params) sits
in `PendingParams` until the next finalization, where it is activated and the next
epoch config snapshot is built from it. The pause state is preserved across
activation: it is emergency-controlled and is not part of the queued economic
update.

## Edge cases

- **Empty active set:** emission is still minted and cumulative advances (when
  not paused); no entitlements are created; the full pool carries forward.
- **Cap reached / subsidy floored to zero:** finalize with zero mint and existing
  carry.
- **Mid-epoch halving:** the subsidy changes at the exact cumulative threshold.
