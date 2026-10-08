---
title: Integrator Guide
---

# Integrator Guide

Where an explorer, indexer or wallet starts: which endpoints a node serves, how to read
state and submit transactions, the transfer rules a wallet must respect, and the events
to project. The complete query list, with each REST path, is the
[CLI & Queries](cli.md) reference; the event list is [Events](events.md).

## How this chain differs from a standard Cosmos chain

- **There is no staking, distribution, governance, mint or slashing module.** Their REST
  routes answer `501 Not Implemented` and their gRPC services do not exist. That is the
  design, not an outage; an explorer must not treat it as an error.
- **Validators come from `x/coreslot`.** There are no delegations, no bonded tokens and
  no unbonding. Each active slot is one validator, and every one has the same voting power
  (`slot_voting_power`, `1` by default); read the set with
  `coreslot-query active` (REST `/twilight/coreslot/v1/active-slots`) and CometBFT's
  `/validators`.
- **Consensus addresses are hex, not bech32.** The CoreSlot queries and events use the
  lowercase hex form of the 20-byte address; CometBFT's `/validators` prints the same
  bytes in uppercase. `coreslot-query by-consensus` accepts either case and refuses a
  bech32 `twilightvalcons…` address with `400`; `coreslot-query reserved` matches only
  lowercase hex, and any other form reads as `404`.
- **Rewards are not claimed.** Each epoch creates one entitlement for each slot that
  earned a nonzero share, held in escrow and released by settlement in `x/mining`; see [Settlement](../rewards/settlement.md).
- **One denomination.** Every amount is in `utwlt`, and rewards and settlement amounts are
  base-10 integer strings. `twlt` (10^6 `utwlt`) is for display only. Account addresses use the
  `twilight` prefix.

Read the chain-id from CometBFT `/status` (`result.node_info.network`) rather than
hard-coding it.

## Endpoints

What a freshly initialized node serves, from its generated `config.toml` and `app.toml`:

| Surface | Default address | On by default | Setting |
|---|---|---|---|
| P2P | `0.0.0.0:26656` | yes | `config.toml` `[p2p] laddr` |
| CometBFT RPC | `127.0.0.1:26657` | yes | `config.toml` `[rpc] laddr` |
| gRPC | `localhost:9090` | yes | `app.toml` `[grpc]` |
| REST (gRPC gateway) | `localhost:1317` | **no** | `app.toml` `[api] enable = true` |
| Swagger UI and spec | on the REST address | **no** | `app.toml` `[api] swagger = true`, needs REST on |
| Prometheus metrics | `:26660` | **no** | see [Monitoring](../operators/monitoring.md) |

Only P2P listens on every interface by default. RPC, gRPC and REST bind to the loopback
interface, so a node serves them to the outside only when its operator changes the
address. To offer them publicly, keep the node's listeners private and put a reverse
proxy in front of them, which is also where browser CORS belongs (`[api]
enabled-unsafe-cors` stays `false`). Do not assume a given node serves `:1317`; keep a
gRPC path available.

- **gRPC is the canonical query API.** Every custom query is a method on
  `twilight.coreslot.v1.Query`, `twilight.rewards.v1.Query` or `twilight.mining.v1.Query`.
- **REST** serves the same queries as GET routes under `/twilight/coreslot/v1`,
  `/twilight/rewards/v1` and `/twilight/mining/v1`, next to the standard `auth`, `bank`,
  `tx`, `consensus`, `upgrade`, `base/tendermint` and `base/node` routes.
- **Swagger** is at `/swagger/` and the merged OpenAPI document at
  `/swagger/twilight.swagger.json`. It covers the three custom modules and the standard
  `auth`, `bank`, `tx`, `consensus`, `base/tendermint` and `base/node` routes. The
  `upgrade` query routes are served but not in the spec, and the absent modules are in
  neither.
- **CometBFT RPC** serves blocks, `/block_results`, `/tx`, `/validators` and `/status`.

## Reading state

- **Pin a height for a pass.** Every state query accepts the `x-cosmos-block-height`
  header (gRPC metadata or HTTP header; `--height` on the command line). Read a whole
  reconciliation at one height; mixing pinned and unpinned reads mixes two moments.
- **`404` is absence; `500` is corruption.** A record that exists but cannot be read is
  never reported as missing, and no response is filled in with a default.
- **Page with the key cursor.** A listing is complete only when `next_key` is empty; an
  empty page with a non-empty `next_key` does not mean the end. Several listings refuse
  `offset`, `count_total` and `reverse`; the list is on [CLI & Queries](cli.md#flags).
- **Use `/active-slots`, not `/slots/active`.** The second is read as a slot id and
  answers `400`.
- **Settlement deadlines are on the settlement clock,** which advances only on blocks
  where release is allowed. Compare a settlement's `deadline_clock` with
  `current_settlement_clock`; a deadline derived from block height or time is wrong across
  a pause.
- **Ask the chain whether an address can be paid.**
  `/twilight/mining/v1/economic-address?address=…` answers `200` with `admissible` and,
  for a refusal, a `rejection_reason` (`…_EMPTY`, `…_INVALID`, `…_MODULE_ACCOUNT`,
  `…_BANK_BLOCKED`). It applies the serving node's current rule, so a pinned height does
  not change its answer.

## Transactions

Every transaction, custom or standard, is broadcast the normal Cosmos way, with each
message packed as an `Any`: gRPC `cosmos.tx.v1beta1.Service/BroadcastTx`, or REST
`POST /cosmos/tx/v1beta1/txs`. No custom message has its own REST route.

The custom message type URLs:

| Module | Messages |
|---|---|
| `x/coreslot` (16) | `/twilight.coreslot.v1.` `MsgRegisterCoreSlot`, `MsgActivateCoreSlot`, `MsgInactivateCoreSlot`, `MsgSuspendCoreSlot`, `MsgRemoveCoreSlot`, `MsgRotateConsensusKey`, `MsgUpdatePayoutAddress`, `MsgUpdateOperatorMetadata`, `MsgUpdateSettlementAddress`, `MsgUpdateSelectionPolicy`, `MsgUpdateParams`, `MsgNominateAuthority`, `MsgAcceptAuthority`, `MsgCancelAuthorityNomination`, `MsgScheduleUpgrade`, `MsgCancelUpgrade` |
| `x/rewards` (3) | `/twilight.rewards.v1.` `MsgUpdateRewardsParams`, `MsgPauseRewards`, `MsgResumeRewards` |
| `x/mining` (2) | `/twilight.mining.v1.` `MsgSubmitSettlementChunk`, `MsgFinalizeSettlement` |

Who may sign each one is on [CLI & Queries](cli.md#transactions). Wallet users mostly send
`/cosmos.bank.v1beta1.MsgSend` and `MsgMultiSend`.

To decode transactions, either ask a node (REST `GET /cosmos/tx/v1beta1/txs/{hash}`
returns the messages as JSON) or decode offline with the descriptor set the repository
publishes at
[`docs/proto/twilight-descriptors.pb`](https://github.com/twilight-project/twilight-core/tree/main/docs/proto).
It bundles the Cosmos transaction envelope, the signer key types, bank and auth, and every
Twilight proto, and the README beside it has a protobufjs example. The repository
publishes no generated TypeScript bindings; a client generates its own from the
descriptor set.

**The signer must already exist on chain.** An address that has never received funds has
no account number or sequence, so it cannot sign anything.

**Fees are not required.** Nodes ship with `minimum-gas-prices = "0utwlt"`, so a
transaction needs a gas limit but no fee. Gas still counts: a large `MsgMultiSend` needs
more than the command line's default limit of 200,000, so set `--gas` (or `--gas auto`).

## Transfer rules

Three rules apply to `MsgSend` and `MsgMultiSend`, and a wallet should check them before
signing:

| Rule | Refused with |
|---|---|
| A transfer that **creates a new account** must carry at least **10,000 `utwlt`** to that account in one transfer. Any amount may go to an account that already exists. | `creating account … requires at least 10000utwlt in a single transfer` |
| **Module accounts cannot receive** bank transfers. | `… is not allowed to receive funds: unauthorized` |
| A transaction may carry at most **32 bank recipient outputs**, counted across all its messages (each `MsgSend` is one, each `MsgMultiSend` output is one). | `transaction carries 33 bank recipient outputs, above the maximum of 32` |

An account is permanent state, which is why creating one has a minimum. Payouts the
protocol makes itself (settlement lines, the remainder to a slot's payout address, the
treasury share) come from the `rewards` module account, which is exempt from the first
rule;
settlement lines have their own floor, `min_recipient_payout_amount`, which is never
below 10,000 `utwlt`. The five module accounts are listed on
[Module Accounts](module-accounts.md).

## Events

Custom events are untyped string events; [Events](events.md) lists every name and
attribute for the three modules. Two things trip indexers:

- **Epoch finalization and validator-set changes happen in EndBlock.** Their events are in
  `/block_results` → `finalize_block_events`, not in any transaction's results.
- **The genesis validator set produces no event.** Seed it from the genesis file or a
  query, then follow `coreslot_validator_update_emitted`.

## Checklist

1. Nothing assumes staking, governance, mint or distribution; a `501` from those routes is
   expected.
2. Validators come from CoreSlot queries and CometBFT `/validators`, with hex consensus
   addresses.
3. Custom messages decode through a node or the descriptor set.
4. Event projections use only the names on [Events](events.md), and read EndBlock events
   from `finalize_block_events`.
5. Rewards are modelled as per-slot entitlements released by settlement, with amounts as
   `utwlt` strings.
6. Reconciliation reads are pinned to one height and follow `next_key` to the end.
7. The wallet applies the three transfer rules before signing.
