---
title: Keys, Backup & Recovery
---

# Keys, Backup & Recovery

What a node and a validator hold, what to copy, what must never be restored from an old
copy, and which recovery paths exist today.

## The keys

| Key | Where | Signs | If lost |
|---|---|---|---|
| **Operator key** | the keyring (`twilightd keys …`; backend `os`, `file`, `kwallet`, `pass`, `test` or `memory`, chosen with `--keyring-backend`; `test` stores keys unencrypted and is for development only) | the slot's own transactions: payout and settlement address, metadata, policy, self-inactivation | the slot's configuration can no longer be changed and new entitlements keep going to the recorded payout address; the authority can still inactivate, suspend or remove the slot, and can rotate its consensus key |
| **Settlement address key** | a keyring, usually on the system that submits participant payouts | settlement chunks and early finalization for the slot ([Settlement](../rewards/settlement.md#chunks)) | the operator installs a new settlement address with `coreslot update-settlement`; a lost key cannot be rotated once the slot is suspended or removed |
| **Authority / emergency keys** | keyrings held by whoever holds the roles; either may be a k-of-n **multisig** account with no chain change | the control surface on the [Authority & Emergency Guide](authority-and-emergency-guide.md#who-can-do-what) | handed over by nomination and acceptance while still held; see the guide |
| **Consensus key** | `config/priv_validator_key.json` on the validator, or a remote signer. With a remote signer the node still generates a key in that file, but it is not the one that signs: take the public key from the signer, not from `comet show-validator`, which prints the file's | blocks | ask the authority for a rotation to a new key ([Validator Operations](validator-operations.md#rotating-your-consensus-key)); the old key is locked out for `consensus_key_reuse_lockout` blocks |
| **Node key** | `config/node_key.json` | nothing on chain; it is the node's p2p identity, the `<node-id>` in peer strings | peers that name your node by id must update their `persistent_peers` |

Payout and settlement addresses are ordinary accounts: not a module account, not the
zero address, and not an address the bank module blocks. Treat the settlement key as a
hot credential — it can direct the slot's open entitlements — and keep the operator key,
which installs it, at least as safe.

## What to back up

Copy, encrypted, somewhere the node cannot reach:

- `config/priv_validator_key.json` (unless a remote signer holds the key — then its
  key store), `config/node_key.json`, the keyring;
- `config/app.toml`, `config/config.toml`, `config/client.toml`, `config/genesis.json`.

`data/priv_validator_state.json` is the double-sign guard: it records the last height,
round and step the consensus key signed, and the node refuses to start without it. Do
**not** keep an old copy of it to restore onto a node that still has its data — an older
copy lets the key sign a height it has already signed. Never run two nodes with the same
consensus key.

## Recovery paths

**Resync from peers — the path that exists.** A node with an empty data directory and
the network's genesis, a reachable `persistent_peers` entry and the right binary joins
by ordinary block sync and reaches the head. Drilled: a fresh, non-validating node from
empty to the head of a four-validator localnet, agreeing on every hash, in about twenty
seconds at height 800; a network at a greater height takes longer. No state-sync
snapshot is involved; `twilightd init` writes `snapshot-interval = 0`, so a node
publishes none unless configured to.

**Rebuilding a validator whose data directory is lost** is the same resync with two
extra rules. The node needs a state file to start: with no data directory, start from
the zeroed one that `twilightd init` or `twilightd comet unsafe-reset-all` writes
(`{"height":"0","round":0,"step":0}`) — this is the one live-chain situation where a
zeroed state file is expected. And the key must not sign any height it signed before the
loss: block sync normally carries the node past every such height before it votes, but
not if the chain has not moved since (a halt, for instance). Set
`double_sign_check_height` in `config.toml` to the number of recent heights the node must
check for its own signatures before it votes, and do not start the validator until the
chain is past the last height the key signed. Reinstall the keys and configuration from
backup first; once caught up, the node votes.

**An export is a record, not a restore.** `twilightd export` captures a chain's complete
state at a height for review, but this binary cannot start a chain from it: every Twilight
module's importer accepts only a fresh genesis ([Upgrade & Export/Import](upgrade-and-export-import.md#an-export-cannot-be-re-imported-by-this-binary)).
A network with no continuation path can only be relaunched from a new genesis, which
ends the running chain's history — which is why in-place upgrades through `x/upgrade`
are the way a live chain changes.

**`unsafe-reset-all`** wipes the databases, deletes the address book and zeroes
`priv_validator_state.json`. It is for a re-genesis
([Node Operator Guide](node-operator-guide.md#joining-an-existing-network)) and for the
lost-data-directory case above — never for a validator that still has its data on a
live chain, where the zeroed state file removes the double-sign guard.

**`rollback`** rolls both CometBFT and application state back one height — blocks are
kept unless `--hard` — so the last block is re-executed on restart. It is for a node
whose own application computed a wrong app hash at that height, a last resort on one
node, never a way to rewrite history the network has committed.
**`prune [default|nothing|everything|custom]`** prunes application-state heights with the
method given on the command line (`default` keeps the last 362,880); it does not read
`app.toml` and does not touch the CometBFT block store.

Restoring a whole network from a snapshot is not supported; recovering a single node is.
