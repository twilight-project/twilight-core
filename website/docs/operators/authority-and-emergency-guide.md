---
title: Authority & Emergency Guide
---

# Authority & Emergency Guide

The chain is governed by **two CoreSlot authorities**, held in CoreSlot params. Rewards
stores no authority of its own — both are read from CoreSlot.

## Who can do what

| Action | Signer | Effect |
|---|---|---|
| Register a slot | **authority** | immediately, as a PENDING slot |
| Activate or remove a slot | **authority** | the next EndBlock |
| Request a consensus-key rotation | **authority** | after `key_rotation_delay_blocks` (default 1: the EndBlock of H+1) for an active slot; at once for a slot that is not active |
| Inactivate a slot | **authority**, or the slot's operator | the next EndBlock; refused if it would take the active set below `min_active_slots` |
| Suspend a slot | **authority** or **emergency authority** | the next EndBlock; refused if it would take the active set below `min_active_slots` unless `allow_emergency_below_min_active`, and never for the last active slot |
| Change CoreSlot params | **authority** | immediately, within the same block; the two authority fields are excluded, and the voting power while any slot is active |
| Schedule or cancel an on-chain upgrade | **authority** | the plan is committed; every node halts at its height ([Upgrade & Export/Import](upgrade-and-export-import.md)) |
| Nominate or cancel a successor for a role | that role's current holder | nothing moves until the nominee accepts |
| Queue a rewards params update | **authority** | the **next epoch boundary**; two inert fields can change ([Parameters](../rewards/params.md#rewards)) |
| Pause or resume rewards | **emergency authority** | the next block (H+1) |
| A slot's payout address, settlement address, metadata | the slot's operator | immediately, within the same block; refused once the slot is suspended or removed |
| A slot's selection policy | the slot's operator | the next block, subject to the cooldown; refused once the slot is suspended or removed |

No signer can mint outside epoch finalization, move escrowed value outside settlement,
redirect a remainder away from the payout address snapshotted at epoch close, or change
the denom, the supply cap, the subsidy or the epoch length. The authority does choose
the validator set, and so who earns future emission; the full picture, including what
each compromised key can do, is on
[Security & Failure Modes](../rewards/security-and-failure-modes.md#authority-model).

## Params update (authority)

```bash
# the query wraps the record in {"params": …}; update-params takes the bare record
twilightd rewards-query params --node <rpc> --output json | jq .params > params.json
# only target_block_time_seconds and max_claim_epochs_per_tx may differ from the current values
twilightd rewards update-params ./params.json --from <authority> \
  --chain-id <chain-id> --node <rpc> --yes
```

The update is queued and applies at the next epoch boundary. Every other field must
keep its current value or the update is rejected: the subsidy, treasury and epoch length
are governed by versioned histories that no transaction writes. See
[Parameters](../rewards/params.md). CoreSlot's own parameters are changed with
`coreslot update-params`, which takes effect immediately.

## Pause / resume (emergency authority)

```bash
twilightd rewards pause  --from <emergency-authority> --chain-id <chain-id> --node <rpc> --yes
twilightd rewards resume --from <emergency-authority> --chain-id <chain-id> --node <rpc> --yes
```

There are no per-area selectors: one canonical pause state stops reward accrual
and release together. A transition accepted in block H takes effect at the
beginning of H+1, before any reward sampling.

Pausing does not stop epoch time. Epoch numbering advances and epochs still
finalize; a fully paused epoch counts zero reward-enabled blocks and emits
nothing. Pausing does not touch pending params or closed epochs.

## Rotating an authority, and seeing a rotation in flight

Handing either authority to a new key is two steps: the current holder
nominates, and the nominee accepts by signing with the new key. Nothing moves
until acceptance, and the holder can withdraw or replace a nomination until then.

```bash
twilightd tx coreslot nominate-authority primary twilight1<successor> --from <authority> ...
twilightd tx coreslot accept-authority primary --from <successor> ...
twilightd tx coreslot cancel-authority-nomination primary --from <authority> ...
```

Between the two steps, the pending-nomination query shows who is nominated for
each role:

```bash
twilightd coreslot-query pending-authority-transfers --node <rpc> --output json
# REST: GET /twilight/coreslot/v1/pending-authority-transfers
```

It returns one entry per role with a nomination pending (`role`,
`transfer.nominee`, `transfer.nominated_height`), primary first. `"transfers": []`
is the ordinary answer: nothing is in flight for either role. It is a success,
never a "not found". The incumbent is `coreslot-query params` at the same height.
The generated `twilightd query coreslot pending-authority-transfers` prints `{}`
instead of `{"transfers":[]}` when nothing is pending; use `coreslot-query` or
REST in scripts.

Both the incumbent and the successor should check this before the successor
accepts: it must show exactly the intended role and nominee.

This view only sees a nomination that **waits**. A key holder can nominate and
accept in the same block, and then no height ever shows it pending. To detect a
rotation that has already happened, compare `coreslot-query params`
(`authority`, `emergency_authority`) against the addresses you recorded, alert on
the `twilight_coreslot_authority_info` gauge (see [Monitoring](monitoring.md)), or
watch for the `coreslot_authority_accepted` event.

## Recovery

- **Accidental pause:** `rewards resume` from the emergency authority. A pause is
  global, so there are no flags to match. It takes effect at the next block;
  blocks produced while paused are not repaid, and the settlement clock was
  frozen for them, so no settlement window was consumed. Epoch finalization never
  stopped: a boundary reached while paused closed its epoch as usual.
- **Bad queued params:** queue a corrected `update-params` before the next
  boundary (the latest queued params win).
- **Key compromise:** rotate the role as described below (nominate a fresh key and
  accept, ideally in the same block). A compromised emergency key can pause (a denial
  of service) and suspend slots; a compromised authority key can change the validator
  set — and with it who earns future emission — change CoreSlot params, and schedule an
  upgrade at a height: cancel any plan you did not make and reverse admissions you did
  not make. Neither can move value already escrowed; see
  [what each key controls](../rewards/security-and-failure-modes.md#what-each-key-controls-and-what-to-do-if-it-is-lost).
- **Authority changed unexpectedly:** if `coreslot-query params` shows an
  `authority` or `emergency_authority` you did not install, the role has
  already moved; there is no timelock, and nominate + accept can land in one
  block. This comparison against your recorded addresses is the check that
  always works.
- **Unexpected nomination:** a pending nomination you did not make (see
  `pending-authority-transfers`, or the
  `twilight_coreslot_pending_authority_nomination` gauge in
  [Monitoring](monitoring.md)) means the role's key is being used by someone
  else — unless it was carried in the launch genesis, which is not evidence of
  a stolen key but should still be canceled. For a suspected stolen key,
  canceling alone is not enough (the same key can nominate again): while you
  still hold the role, nominate a fresh key you control and have it accept,
  ideally back to back in the same block. The new nomination replaces the
  pending one.

## What not to do

:::warning
- Do not expect `rewards update-params` to change the subsidy, the treasury share or
  address, the epoch length, the denom or the cap — every one is rejected; only
  `target_block_time_seconds` and `max_claim_epochs_per_tx` may change.
- Do not enable fees or weighted rewards — rejected by validation.
- Do not assume a pause takes effect at the next epoch — pause/resume are
  **immediate**, params updates are **queued**.
:::
