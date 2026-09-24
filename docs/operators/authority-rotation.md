# Rotating an authority

Audience: whoever holds the authority key, or is about to.

Twilight Core has two operational authority roles, both ordinary signable accounts recorded in
`x/coreslot` parameters:

| role | CLI name | gates |
|---|---|---|
| primary | `primary` | validator admission, parameter updates, upgrade scheduling |
| emergency | `emergency` | pausing and resuming rewards |

Handing either to another key is a **two-step** operation. The incumbent nominates; the
successor accepts by signing with the key it must actually hold. Nothing changes until it does.

Reproduce the whole flow with:

```bash
make localnet-authority-rotation-drill
```

---

## Why two steps

A single-step rotation is unrecoverable when it goes wrong, and it goes wrong in ordinary ways.

The primary authority gates validator admission, parameter updates, **and upgrade scheduling** —
`ScheduleUpgrade` reads the same parameter from chain state. So a chain that loses its authority
cannot upgrade its way out of having lost it. There is no governance module to appeal to.

Requiring the destination to sign means a wrong-but-valid address can never take the role. A typo
is inert and correctable rather than terminal.

**What this does not do:** it does not protect you against someone who already holds your
authority key. There is no timelock, so an attacker nominates an address they control and accepts
— in the same block, if both transactions are sent together. You are guaranteed no reaction
window. Protect the key itself — a k-of-n multisig account works here with no chain change.

---

## Rotating

### 1. Nominate

Signed by the **current holder** of that role:

```bash
twilightd tx coreslot nominate-authority primary twilight1<successor> \
  --from <current-authority-key> --chain-id <chain-id>
```

Nothing has changed yet. The incumbent still holds every capability, and the nominee holds none.
Verify that before continuing — an incumbent that has already lost the role is a different and
much worse situation than a pending nomination:

```bash
twilightd coreslot-query pending-authority-transfers --output json
twilightd coreslot-query params --output json | jq '.params | {authority, emergency_authority}'
```

The first must show exactly the nomination you intended — right role, right nominee — and the
second must still show the incumbent. The successor should run the same check independently
before accepting, rather than accepting on the incumbent's word. See [Verifying](#verifying).

The nominee is checked at this point. A module account, a bank-blocked address or the all-zero
address is refused outright: nobody can sign for those, so installing one would end the role
permanently.

### 2. Accept

Signed by the **nominee**, not the incumbent:

```bash
twilightd tx coreslot accept-authority primary \
  --from <successor-key> --chain-id <chain-id>
```

On success the role transfers, the pending nomination is cleared, and the former holder loses the
capability immediately.

The successor account must exist on chain before it can sign. Fund it first, even with a trivial
amount — an unfunded key cannot submit anything, and the failure looks like an authorization
error rather than a missing account.

### Changing your mind

Either withdraw the nomination:

```bash
twilightd tx coreslot cancel-authority-nomination primary --from <current-authority-key> ...
```

or simply nominate someone else — a new nomination replaces the pending one for that role, and
the displaced nominee can no longer accept.

Both are signed by the current holder, which is what makes a mistaken nomination survivable.

---

## What cannot happen

- **A nomination cannot move the role.** Only acceptance does.
- **Only the exact nominee can accept.** Not the incumbent, not a previous nominee.
- **`update-params` cannot rotate anything.** A parameter document whose `authority` or
  `emergency_authority` differs from current state is **rejected**, not silently corrected. This
  is the case the design exists for: before it, editing `max_active_slots` meant re-supplying both
  authority addresses correctly, and getting one wrong was unrecoverable.
- **The two roles are independent.** Rotating one leaves the other untouched, and the holder of
  one cannot nominate for the other.

---

## Verifying

A completed rotation is visible in parameters:

```bash
twilightd coreslot-query params --output json | jq '.params | {authority, emergency_authority}'
```

A rotation **in flight** — nominated, not yet accepted or canceled — is visible in the
pending-nomination query, for both roles at once:

```bash
twilightd coreslot-query pending-authority-transfers --output json
# the same, REST:             curl $REST/twilight/coreslot/v1/pending-authority-transfers
```

The generated tree has the same query (`twilightd query coreslot pending-authority-transfers`),
but when nothing is pending it prints `{}` rather than `{"transfers":[]}`: its encoder drops an
empty list. It still shows every pending entry, but scripts should use `coreslot-query` or REST,
whose empty answer is explicit.

```json
{
  "transfers": [
    {
      "role": "AUTHORITY_ROLE_PRIMARY",
      "transfer": { "nominee": "twilight1<successor>", "nominated_height": "1234" }
    }
  ]
}
```

- One entry per role with a nomination pending, primary first. A role with no entry has no
  handover in flight.
- `"transfers": []` means **nothing is pending for either role**. It is a successful answer, not
  an error; the query never answers "not found".
- `nominated_height` is the block the nomination was included in (for a nomination carried in
  genesis, whatever height the document stated). Add `--height <h>` to see
  what was pending at an earlier height, and read `params` at the same height for the incumbent.
  A height the node has pruned is an error, not an empty answer — do not read it as "nothing
  was pending".
- The nominating address is not stored. It is the incumbent at `nominated_height`, and it is also
  in that transaction's `coreslot_authority_nominated` event.

### Detecting a rotation you did not make

**The check that always works is the holder itself.** Record the authority and emergency
addresses you expect, and compare them against the chain:

```bash
twilightd coreslot-query params --output json | jq '.params | {authority, emergency_authority}'
```

Any difference is a rotation that happened. Every completed handover also emits a
`coreslot_authority_accepted` event (`authority_role`, `previous_authority`, `authority`), so an
indexer or event subscriber can catch the moment it happens.

**The pending query, and its gauge, only see a nomination that waits.** Nodes export
`twilight_coreslot_pending_authority_nomination{role}`, which is 1 while a nomination is pending,
and this query shows **who** is nominated. That catches the honest two-step, a nomination carried
in genesis, and an attacker who nominates and waits. It does **not** catch someone who holds the
key and rotates in one go: a nomination and its acceptance can land in the **same block**, and
then no committed height ever shows a pending entry and the gauge never leaves 0. Use the pending
view to confirm your own handovers and to spot a waiting one; use the holder comparison above to
detect a completed one.

**If an entry appears that you did not expect**, find out where it came from before acting:

- A nomination carried in a **launch genesis** is not evidence of a stolen key — it was in the
  document the chain started from. It is still dangerous (its nominee can accept at any height),
  so withdraw it with `cancel-authority-nomination` and fix whatever let it through sign-off.
- Otherwise a nomination can only have been made by the role's current holder, so treat the key
  as being used by someone else. Cancelling is not enough: the attacker holds the same key and can
  simply nominate again. Instead, **nominate a fresh key you control and have it accept**,
  preferably both transactions back to back so they land in the same block. The new nomination
  replaces the pending one in a single transaction, and once the fresh key accepts, the stolen key
  no longer holds the role. This only works while the incumbent key still holds the role — check
  `params` first.

> **Note on `update-params`:** the output of `coreslot-query params` cannot currently be fed
> straight back into `coreslot update-params` — the query renders numbers as JSON strings and the
> command expects unquoted numbers. Convert them before submitting. Tracked separately.

---

## Genesis

A fresh genesis should carry no pending nominations, and `coreslot-genesis set-authorities` sets
both roles directly. Genesis validation does **not** require the list to be empty, so check
`app_state.coreslot.pending_authority_transfers` is `[]` before signing off on a launch genesis,
and run `coreslot-query pending-authority-transfers` once the chain is up: a nomination carried in
genesis can be accepted by its nominee at any height.

Note that a genesis produced by plain `twilightd init` seeds both fields with **module
addresses**, which nobody can sign for — a chain launched without running `set-authorities` is
ungovernable from block one.

Pending nominations survive export and import, so a captured state does not strand a rotation
that was in flight.
