# Maintainers

| GitHub handle | Role |
| --- | --- |
| [@usmanshahid86](https://github.com/usmanshahid86) | Lead maintainer |

## Responsibilities

Maintainers are responsible for:

- **Review of consensus-critical paths** — `x/coreslot`, `x/rewards`, `x/mining`, `app/`
  wiring, upgrade handlers, and genesis import/export, as described in
  [`REVIEW.md`](REVIEW.md).
- **Releases** — cutting release tags on `main`, publishing binaries and checksums, and
  registering upgrade handlers, as described in [`CONTRIBUTING.md`](CONTRIBUTING.md).
- **Security triage** — handling reports received through GitHub Private Vulnerability
  Reporting, as described in [`SECURITY.md`](SECURITY.md).
- **Community conduct** — enforcing the [Code of Conduct](CODE_OF_CONDUCT.md).

While there is a single maintainer, the `main` ruleset requires zero approvals, because a sole
maintainer cannot approve their own pull request. Code-owner routing and required approvals
will be added once there is more than one maintainer.

## Becoming a maintainer

Maintainers are invited by the existing maintainers. The usual path is a sustained record of
high-quality contributions and reviews — particularly on consensus-critical code — and
demonstrated care for the determinism, fail-closed, and security rules in
[`AGENTS.md`](AGENTS.md) and [`REVIEW.md`](REVIEW.md). If you are interested, say so in an
issue or pull request conversation with a maintainer.
