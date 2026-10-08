---
title: Keys, Backup & Recovery
---

# Keys, Backup & Recovery

What a node and a validator hold, what to copy, what must never be restored from an old
copy, and which recovery paths exist today.

## The keys

| Key | Where | Signs | If lost |
|---|---|---|---|
| **Operator key** | the keyring (`twilightd keys …`; backend `os`, `file` or `test`, chosen with `--keyring-backend`; `test` stores keys unencrypted and is for development only) | the slot's own transactions: payout and settlement address, metadata, policy, self-inactivation | the slot's configuration can no longer be changed and new entitlements keep going to the recorded payout address; the authority can still inactivate, suspend or remove the slot, and can rotate its consensus key |
| **Settlement address key** | a keyring, usually on the system that submits participant payouts | settlement chunks and early finalization for the slot ([Settlement](../rewards/settlement.md#chunks)) | the operator installs a new settlement address with `coreslot update-settlement`; a lost key cannot be rotated once the slot is suspended or removed |
| **Authority / emergency keys** | keyrings held by whoever holds the roles; either may be a k-of-n **multisig** account with no chain change | the control surface on the [Authority & Emergency Guide](authority-and-emergency-guide.md#who-can-do-what) | handed over by nomination and acceptance while still held; see the guide |
| **Consensus key** | `config/priv_validator_key.json` on the validator, or a remote signer (then that file holds no key) | blocks | ask the authority for a rotation to a new key ([Validator Operations](validator-operations.md#rotating-your-consensus-key)); the old key is locked out for `consensus_key_reuse_lockout` blocks |
| **Node key** | `config/node_key.json` | nothing; it is the node's p2p identity, the `<node-id>` in peer strings | peers that name your node by id must update their `persistent_peers` |

Payout and settlement addresses are plain accounts: anything that can hold `utwlt` and
is not a module account. Treat the settlement key as a hot credential — it can direct the
slot's open entitlements — and keep the operator key, which installs it, at least as
safe.

## What to back up

Copy, encrypted, somewhere the node cannot reach:

- `config/priv_validator_key.json` (unless a remote signer holds the key — then its
  key store), `config/node_key.json`, the keyring;
- `config/app.toml`, `config/config.toml`, `config/client.toml`, `config/genesis.json`.

Do **not** keep an old copy of `data/priv_validator_state.json` as something to restore:
it records the last height and round the consensus key signed, and starting a validator
with an older copy lets it sign a height it has already signed — a double sign. Never run
two nodes with the same consensus key. If a validator's data directory is lost, resync
from empty state (below): the node only starts voting once it has reached the head, so
every height it signs is new.

## Recovery paths

**Resync from peers — the path that exists.** A node with an empty data directory and
the network's genesis, a reachable `persistent_peers` entry and the right binary joins
by ordinary block sync and reaches the head. Drilled: from empty to the head of a
four-validator localnet, agreeing on every hash, in about twenty seconds at height 800;
a long-running network takes longer in proportion to its height. Reinstall your keys and
configuration from backup first and the node validates again as soon as it is caught up. No state-sync snapshot is involved; the `snapshots`
commands exist in the binary but no network publishes snapshots today.

**An export is a record, not a restore.** `twilightd export` captures a chain's complete
state at a height for review, but this binary cannot start a chain from it: every module's
importer accepts only a fresh genesis ([Upgrade & Export/Import](upgrade-and-export-import.md#an-export-cannot-be-re-imported-by-this-binary)).
A network with no continuation path can only be relaunched from a new genesis, which
ends the running chain's history — which is why in-place upgrades through `x/upgrade`
are the way a live chain changes.

**`unsafe-reset-all` is for a re-genesis only.** It wipes the databases and zeroes
`priv_validator_state.json`; run it when the network restarts from a new genesis
([Node Operator Guide](node-operator-guide.md#joining-an-existing-network)), never to
"fix" a validator on a live chain.

**`rollback`** reverts the application state by one block, for the case where CometBFT
has persisted an app hash the application disagrees with. It is a last resort on one
node, not a recovery procedure, and never a way to rewrite history the network has
committed. **`prune`** reclaims disk from old heights according to the node's pruning
settings.

Restoring a whole network from a snapshot is not supported; recovering a single node is.
