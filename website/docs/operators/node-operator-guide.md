---
title: Node Operator Guide
---

# Node Operator Guide

Practical steps to build, initialize, and run a `twilightd` node. For multi-node
localnets see [Localnet](../chain/localnet.md).

## Build

```bash
make build           # ./build/twilightd
```

## Initialize

```bash
twilightd init <moniker> --chain-id <chain-id>
```

The generated genesis includes the rewards default genesis (rewards is registered
in the CLI basic manager). Inspect with:

```bash
jq '.app_state.rewards' ~/.twilightd/config/genesis.json
jq '.app_state.coreslot' ~/.twilightd/config/genesis.json
```

## Mempool bounds

`twilightd init` writes a `config.toml` that holds a smaller mempool than the
CometBFT default: `max_txs_bytes` is **88080384** bytes — four blocks' worth of
transaction data — rather than 1 GiB. The transaction count (`mempool.size`,
5000) and the per-transaction limit (`max_tx_bytes`, 1 MiB) keep their upstream
values, because at 5000 transactions the count already binds well below one
block's worth of ordinary traffic.

Transactions on this chain carry no fee, and the mempool is first-in-first-out
with no per-sender fairness. The depth of the queue is therefore how long a
transaction can sit behind somebody else's backlog, and 1 GiB against a 21 MiB
block is roughly forty-eight blocks of it. A few blocks absorbs a burst; tens of
blocks is a wait an operator would read as an outage.

What this setting does **not** do:

- It is **node-local**. It bounds your node's queue and nothing else. A validator
  running different settings is unaffected by yours, so this is an operational
  control over your own node, not a chain-level limit.
- It is **not per-sender fairness** and **not an economic limit**. A shallower
  queue shortens the wait; it does not divide the queue between senders.
- Raising `minimum-gas-prices` in `app.toml` does not substitute for it. That
  value is applied when your node accepts a transaction into its mempool, not
  when the chain executes a block, so it changes what your node relays rather
  than what the chain accepts. It ships as `0utwlt` deliberately.

**Nodes initialized earlier keep their old value.** The setting is written only
when `config.toml` does not exist yet, so an existing node needs the edit by
hand:

```bash
grep max_txs_bytes ~/.twilightd/config/config.toml
```

```toml
[mempool]
max_txs_bytes = 88080384
```

Restart the node after editing.

## Start

```bash
twilightd start
```

For a four-node localnet, use the scripts:

```bash
scripts/localnet/init.sh
scripts/localnet/start.sh
scripts/localnet/stop.sh
```

## Joining an existing network

A full node on a network someone else runs needs three things from that network's
operators: its **chain-id**, its **genesis document** (a published file, or the
`/genesis` route of one of its nodes), and at least one **peer address** of the form
`<node-id>@<host>:26656`.

```bash
twilightd init <moniker> --chain-id <chain-id>

# Install the network's genesis: the published file...
curl -fsSL <genesis-url> -o ~/.twilightd/config/genesis.json
# ...or fetched from one of its nodes:
# curl -s http://<rpc-host>:26657/genesis | jq '.result.genesis' > ~/.twilightd/config/genesis.json

jq -r '.chain_id' ~/.twilightd/config/genesis.json   # must print <chain-id>
twilightd validate-genesis ~/.twilightd/config/genesis.json

# Peer with the network (GNU sed shown):
sed -i 's#^persistent_peers =.*#persistent_peers = "<node-id>@<host>:26656"#' \
  ~/.twilightd/config/config.toml
```

Then start the node. For anything longer-lived than a test, run it under a service
manager so it restarts after a crash and survives a reboot. First install the binary
built above where the unit expects it (from the repository root):

```bash
sudo install -m 0755 build/twilightd /usr/local/bin/twilightd
```

A minimal systemd unit:

```ini
# /etc/systemd/system/twilightd.service
[Unit]
Description=Twilight full node
After=network-online.target
Wants=network-online.target

[Service]
User=<user>
ExecStart=/usr/local/bin/twilightd start
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now twilightd
journalctl -u twilightd -f
```

The node is joined when it reports `catching_up: false` and its height tracks the
network's:

```bash
curl -s http://localhost:26657/status \
  | jq '.result.sync_info | {height: .latest_block_height, catching_up}'
```

**Becoming a validator.** Validators are admitted by the chain authority, not by
stake. Give the authority your operator address, a payout address, a settlement
address (any of the three may be the same account), your consensus public key
(`twilightd comet show-validator | jq -r .key`) and a moniker. Once the authority
registers and activates the slot, the running node starts validating without a
restart; `/status` then shows a non-zero `validator_info.voting_power`.

**Re-joining after a re-genesis.** When a network restarts from a new genesis, stop
the node and run `twilightd comet unsafe-reset-all --home ~/.twilightd`. This wipes
the chain databases, zeroes `data/priv_validator_state.json` and deletes the address
book (`config/addrbook.json`), but keeps your keys and configuration, so
`persistent_peers` in `config.toml` still names your peers. Then replace
`config/genesis.json` with the new document and start again.

If the new genesis also carries a new chain-id, update `chain-id` in
`config/client.toml`. Nothing rewrites it after the first `init`, and the CLI signs
transactions for the chain-id that file names unless `--chain-id` is passed:

```bash
sed -i 's#^chain-id = .*#chain-id = "<new-chain-id>"#' ~/.twilightd/config/client.toml
```

**`twilightd init` does not reset existing configuration.** Run over a home that
already has an `app.toml` and `config.toml`, it keeps their values and updates only
the moniker, so settings from an earlier deployment (pruning, `min-retain-blocks`, API
enablement, peers) carry over silently. Review them after re-initialising a home
directory. It also leaves `client.toml` alone (see above), and it refuses to run while
`config/genesis.json` exists (`genesis.json file already exists`) unless you pass
`--overwrite`, which replaces that file with a fresh default genesis. Install the
network's genesis again afterwards.

## Check status and agreement

```bash
curl -s http://<rpc-host>:26657/status | jq '.result.sync_info.latest_block_height'
# Cross-node hash agreement (localnet):
scripts/localnet/agree.sh
```

`agree.sh` confirms all nodes agree on **app hash**, **validators hash**, and
**next-validators hash** at a common height. App-hash divergence is a state fork —
treat as critical (see [Incident Response](incident-response.md)).

## Logs

Localnet logs are written under the network home (e.g.
`/tmp/twilight-rewards-localnet/logs`). Watch for repeated EndBlock errors — the
rewards module is fail-closed and will halt the block on a finalization fault (see
[Security & Failure Modes](../rewards/security-and-failure-modes.md)).

## Common failures

| Symptom | Check |
|---|---|
| Node won't start | genesis validity (`twilightd validate-genesis`); port conflicts |
| Halts at a height with EndBlock error | rewards finalization fault — inspect logs; this is fail-closed by design |
| App-hash divergence vs peers | **critical** — stop and investigate a possible fork |
| Empty validator set at InitChain | genesis must include at least one active CoreSlot |
