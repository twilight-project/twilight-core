# Review Process

Twilight Core is consensus- and value-critical, so every change is reviewed before it
lands. This document describes how — what the `main` branch ruleset enforces, the automated
gates, and the multi-model adversarial review used heavily during early development and,
going forward, alongside maintainer review.

## Process history (full transparency)

The chain — including the consensus (`x/coreslot`) and rewards (`x/rewards`) modules — was
initially developed **solo, in-house**, with the multi-model AI review described below run
continuously during development. That early review was **not recorded per-change** (some
work was merged locally without a PR).

**From the adoption of this document onward, all changes are reviewed on the record**
(PRs + CI + the checklist below). The already-built modules are additionally covered
retroactively by:

- a **recorded baseline review** of `x/coreslot` and `x/rewards`
  ([docs/reviews/2026-06-29-baseline-coreslot-rewards.md](docs/reviews/2026-06-29-baseline-coreslot-rewards.md)),
  with supporting evidence indexed in
  [docs/testing/validation-summary.md](docs/testing/validation-summary.md), and
- the going-forward automated gates, simulations, and chaos drills, which run against the
  current code regardless of how it was originally merged.

We deliberately do **not** fabricate back-dated issues/PRs or rewrite history. For code,
the assurance that matters is the *current* state plus the recorded review and tests
attached to it.

## What the `main` ruleset enforces

These are enforced by GitHub on the `main` branch, not merely expected:

- **Pull requests required** — changes reach `main` only through a pull request.
- **Required status checks** — the six CI checks below must pass before merge.
- **Merge commits only** — squash and rebase merges are not allowed, so a reviewed head
  stays identifiable in the history.
- **No force-push and no deletion** of `main`.

**Required approvals are currently zero.** The project has a single maintainer (see
[`MAINTAINERS.md`](MAINTAINERS.md)), and a sole maintainer cannot approve their own pull
request, so requiring an approval would block every change. Maintainer review is therefore a
process expectation (see [Human review](#human-review)), not a ruleset guarantee. Code-owner
routing and required approvals will be added once there is more than one maintainer.

## Automated gates (required CI checks)

Every PR must pass [`.github/workflows/ci.yml`](.github/workflows/ci.yml):

- **build & test** — `go build ./...`, `go test ./...`, plus the harness fault checks
  (release-upgrade rehearsal, block-gas drill, genesis verifier), the vulncheck
  toolchain-pin check, and the CLI-surface check in the same job
- **consensus vectors** — conformance against the normative protocol vector packs
- **golangci-lint** — static analysis (`.golangci.yml`)
- **gofmt & tidy** — formatting and a clean `go mod tidy`
- **proto descriptor up to date** — the committed descriptor set matches a pinned-`protoc`
  regeneration
- **govulncheck** — dependency vulnerability scan; **blocking** (a newly reachable
  advisory fails CI; advisories in modules the code does not call do not)

An advisory that is reachable but has **no available fix** — for example one in an
unmaintained upstream package pulled in transitively — would otherwise leave the gate with no
achievable green state. Such advisories are accepted explicitly in
[`.govulncheck-allow.json`](.govulncheck-allow.json), each with its reachability path, the
reason it cannot be fixed, and a **`review_by` date**. Anything reachable and not listed there
still fails, and an acceptance that passes its `review_by` date also fails, so an exception
cannot outlive its review. An advisory the Go vulnerability database later **withdraws** is
removed from the allowlist, not re-justified: there is nothing left to accept, and
`scripts/vulncheck.sh` reports any entry that is no longer reachable. `make vuln` and CI run the same script
([`scripts/vulncheck.sh`](scripts/vulncheck.sh)), so local and CI results cannot drift.

Consensus/economic changes should also be exercised by the relevant **drills**
(`make drills`) and, as they land, the **module simulations**.

## Multi-model adversarial review

Changes go through a layered review designed to decorrelate blind spots as far as
practical with current tooling:

1. **Broad correctness pass** — a general LLM review of the change for correctness,
   clarity, and obvious defects.
2. **Adversarial/hostile pass** — a reviewer prompted to actively hunt for bugs, edge
   cases, and unsafe assumptions, treating the change as guilty until proven correct.
3. **Self-review pass** — an automated self-review before the change is opened, catching
   regressions and style/contract violations.
4. **PR review** — an automated reviewer on the pull request as the final gate.

This is a strong **defect-removal** layer, but it does **not** replace maintainer
responsibility, deterministic tests, simulations, operational drills, or independent expert
review. Emergent, system-level properties — consensus safety under adversarial validators,
economic-incentive attacks, and cross-module invariants — are validated separately by
simulations and chaos drills, and, before mainnet, by an **independent expert review and
security audit** (see [`SECURITY.md`](SECURITY.md)).

## Human review

Consensus-critical changes are expected to receive a **maintainer review**, alongside the
adversarial review above, before they merge. While there is a single maintainer this is a
process commitment rather than an enforced approval (see
[What the `main` ruleset enforces](#what-the-main-ruleset-enforces)). Consensus-critical
areas include:

- `x/coreslot` validator-set and lifecycle logic;
- `x/rewards` finalization, emission, and economic accounting;
- `x/mining` finalization and settlement;
- `app/` wiring;
- upgrade handlers;
- genesis import/export behavior;
- any code path that can affect deterministic state transitions or `ValidatorUpdate`s.

Once there is more than one maintainer, the ruleset will route these paths to code owners
and require at least one approving review from someone other than the author.

## Reviewer checklist

See the [pull request template](.github/PULL_REQUEST_TEMPLATE.md) for the per-change
checklist (tests, determinism, fail-closed posture, validator-update provenance, security
considerations, migration/upgrade impact, and docs).
