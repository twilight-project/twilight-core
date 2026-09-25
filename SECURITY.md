# Security Policy

Twilight Core is a Cosmos SDK / CometBFT Proof-of-Authority blockchain. Consensus,
validator admission through `x/coreslot`, validator-set updates, and reward/economic
accounting are security-critical.

A vulnerability may halt the chain, fork it, corrupt state, manipulate the validator set,
or incorrectly issue, account for, or pay `utwlt`. We take security disclosures seriously
and appreciate responsible reporting.

## Reporting a vulnerability

**Do not open a public issue, pull request, or discussion for a newly discovered
vulnerability.**

Report it privately through **GitHub Private Vulnerability Reporting**, the project's only
security channel: on this repository, go to **Security → Report a vulnerability**, or open
<https://github.com/twilight-project/twilight-core/security/advisories/new> directly. This
creates a private advisory visible only to maintainers. There is no security email address.

Please include as much of the following as possible:

- A description of the issue and its impact.
- The affected module, component, commit, branch, or release.
- Reproduction steps or a proof of concept.
- A failing test, localnet scenario, or drill, if available.
- Any suggested remediation.
- Whether the issue may affect consensus safety, liveness, validator admission, token
  accounting, private data, or operator security.

## What to expect

- **Acknowledgement within 3 business days.**
- **Initial triage within 10 business days** — a severity assessment and whether we can
  reproduce the report, with follow-up questions where needed.
- Updates while a fix is prepared.
- **Coordinated disclosure after a fix ships**, once operators have had a reasonable window
  to upgrade.

We will credit the reporter in the advisory unless they prefer to remain anonymous.

## Public testnet policy

Twilight Core currently runs as a **public testnet whose tokens have no value**. On that
basis:

- **Known limitations** of the testnet are tracked **openly as GitHub issues**, so that
  their resolution is visible and verifiable. Opening or discussing such a tracking issue is
  expected, not a disclosure violation.
- **Newly discovered vulnerabilities** must still be reported **privately** through Private
  Vulnerability Reporting, even on the testnet. If you are unsure whether something is
  already a known, publicly tracked limitation, report it privately.
- **Before any mainnet or other real-value network**, every open security-relevant issue is
  reviewed and resolved.

## Scope

A security issue in this repository is in scope if it can affect:

- consensus safety or liveness;
- deterministic state execution;
- validator admission, removal, rotation, or active-set updates;
- `ValidatorUpdate` provenance;
- reward, emission, or token accounting;
- genesis import/export correctness;
- transaction validation or authorization;
- CLI, REST, or gRPC behavior that can cause unsafe state changes or unsafe operator
  behavior;
- exposure of secrets, private keys, validator keys, mnemonics, RPC credentials, or
  sensitive operational data.

Examples of in-scope areas: `x/coreslot`; `x/rewards` (reward, emission, and economic
accounting); `x/mining`; `app/` wiring; keeper logic; message handlers; BeginBlock and
EndBlock handlers; genesis handling; validator-set update paths; and security-sensitive CLI,
REST, and gRPC surfaces.

## Out of scope

The following are generally out of scope:

- third-party infrastructure not controlled by the project;
- attacks requiring control of a reporter's own node, host, or a non-default deployment;
- public testnet operational-host issues that do not indicate a vulnerability in Twilight
  Core;
- generic denial-of-service against a single unhardened node where no protocol or
  implementation vulnerability is demonstrated;
- social engineering;
- spam, phishing, or abuse reports unrelated to this codebase;
- issues already tracked publicly.

If uncertain, report privately rather than opening a public issue.

## Supported versions

Twilight Core is **pre-1.0** and runs as a **public testnet**. Security fixes are made on
`main` (the trunk) and shipped in the supported release line:

| Version | Status |
| --- | --- |
| `v0.3.0` release candidates (`v0.3.0-rc*`) | **Supported** for security fixes |
| `v0.2.0` | Best effort — superseded by the `v0.3.0` line |
| `v0.1.0` and older builds | Unsupported |

Releases are published on the
[releases page](https://github.com/twilight-project/twilight-core/releases).

## Safe harbor

We will not pursue or support action against anyone for security research conducted in good
faith under this policy — research that respects the privacy of others, avoids degrading or
disrupting network service, does not destroy or modify data that is not their own, and is
reported privately through the channel above without public disclosure before a fix ships.
If in doubt about whether an activity is covered, ask through the same private channel
first.

## Audits and bounty

Twilight Core has **not been externally audited**. It undergoes continuous internal review,
including automated CI and multi-model adversarial review (see [`REVIEW.md`](REVIEW.md));
that process is **not** a substitute for an independent security assessment.

There is **no bug bounty** at present. An independent third-party security audit is planned
before any mainnet or real-value network; this section will link the audit report, and any
bounty scope and terms, when available.
