# Contributing to Twilight Core

Thanks for your interest in Twilight Core — a Cosmos SDK / CometBFT Proof-of-Authority
chain with three custom modules:

- **`x/coreslot`** — validator admission and the validator set.
- **`x/rewards`** — the emission economy: epoch emission and per-`(slot, epoch)`
  entitlements.
- **`x/mining`** — the settlement workflow: a block-driven settlement clock, settlement sets
  materialized at epoch close, chunked payouts, and finalization releasing the remainder to
  the recorded payout address. It holds no bank keeper; value moves only through
  `x/rewards`.

The standard staking, slashing, gov, mint, and distribution modules are **intentionally
omitted** (not wired into the app) — the validator set and token economics are handled
entirely by these custom modules.

This is a young project under active development. Contributions are welcome; because the
code is consensus- and value-critical, the bar for changes to `x/coreslot`, `x/rewards`,
`x/mining`, and `app/` is high.

## Before you start

- For anything non-trivial, **open an issue first** to discuss the approach.
- For a **newly discovered security vulnerability, do not open an issue** — report it
  privately as described in [`SECURITY.md`](SECURITY.md). Known testnet limitations are
  tracked openly as issues; see the public testnet policy there.
- Read the design background in [`docs/architecture/`](docs/architecture/) (ADRs) and the
  module docs on the documentation site under [`website/`](website/).

## Development setup

Builds with **Go 1.26.9**, the `toolchain` line in `go.mod`; with Go 1.25.13 or newer installed, the
`go` command downloads it automatically. The `go` directive (1.25.13) is only the minimum for a module
that imports this one.

```bash
make build     # stamped binary at build/twilightd
make test      # go test ./...
make fmt       # gofmt
make lint      # golangci-lint (matches CI)
make proto     # regenerate protobuf (only if you changed proto/)
```

Local end-to-end and chaos checks:

```bash
make localnet-smoke           # 4-node localnet sanity
make localnet-rewards-epoch-smoke  # rewards epoch finalization + entitlements
make drills                   # lifecycle + restart-rotation + quorum drills
```

## Branching & commits

- Work on a feature branch off **`main`**, and open a PR back into `main`. There is no
  long-lived integration branch: `main` is the trunk. Changes reach `main` only through a
  pull request, and the repository allows **merge commits only** (squash and rebase merges
  are disabled) so a reviewed head stays identifiable in the history.
- A release is a **tag on `main`**, not a branch promotion.
- Use **[Conventional Commits](https://www.conventionalcommits.org/)** — e.g.
  `feat(rewards): ...`, `fix(coreslot): ...`, `docs: ...`, `chore(ci): ...`.
- Keep PRs focused and reviewable; describe the *why*, not just the *what*.
- If you changed `proto/`, regenerate with `make proto` and commit the result.
- If you changed behavior, add/adjust tests; consensus/economic changes should also be
  covered by a drill or simulation where practical.

## Releases

Version classes follow the two procedures in
[ADR-0003 §3](docs/architecture/adr/0003-upgrade-path.md) — the ADR defines the procedures,
the numbering below is this project's convention for them:

- **minor** (`v0.2.0`, `v0.3.0`, …) — a state-machine change. Ships a registered upgrade
  handler **named after the version it upgrades to**, and needs a coordinated halt.
- **patch** (`v0.1.1`, `v0.2.1`, …) — node-local only: pruning, RPC, indexer, p2p,
  telemetry, dependencies. No upgrade handler; operators roll one at a time.

A line's first tag without an `-rc` suffix is its **final**: the line's state machine is
settled, nothing pending would change a block on it, and every remaining state-machine item
belongs to the next minor. A final is not a readiness claim; readiness is stated in
[`README.md`](README.md), [`SECURITY.md`](SECURITY.md) and the status page. Release
candidates (`-rc1`, `-rc2`, …) exist only while a line's state machine may still change
before its final; once the final is tagged, node-local changes on that line ship as patches,
never as further candidates. `v0.3.0` was the first line to follow this rule (its `-rc1`
through `-rc5` predate it).

`v0.1.0` is the **first proven upgrade-capable operational baseline** — the first version
carrying `x/upgrade`, with the upgrade proven end to end across four validators and
export/restore/join characterized. It was **not** a public-testnet or genesis release: at that
point the two-step authority rotation added in #130 did not exist, and none of the anti-spam
findings had been addressed. Nothing upgrades *to* it, so it registers no handler.

Those findings are now tracked individually rather than as one cluster, because they resolve
by different mechanisms and on different timelines:

- **TW-006** — permanent account growth. **Closed.** The minimum-funding send restriction and
  the bank-output cap are merged and registered under the `v0.3.0` boundary.
- **TW-005** — feeless mempool admission. **Mitigated, not closed.** The backlog a node will
  queue is bounded by configuration (#164). That bound is node-local: it binds the nodes an
  operator runs, and it is not consensus-enforced per-sender fairness.
- **TW-004** — unlimited block gas. **Open.** A finite `block.max_gas` can be set at genesis,
  but not by transaction: consensus parameters are unreachable from any signable message on
  this chain (#167). On a running network, a named upgrade's handler can set them with
  `app.SetBlockParams` (#170), so changing one is a coordinated upgrade.
  Calibrating a value against representative hardware (#160) and establishing the
  legitimate-gas floor (#107) both remain open.

Earlier commits carry descriptive tags rather than version numbers, because a chain launched
from a build without `x/upgrade` can never be upgraded, and numbering such a build would imply
a migration path that does not exist. That is an argument about *numbering*, not about
readiness: being the first build a public network could legitimately be launched *from* is not
the same as being ready to launch one.

The handler registry in `app/upgrades.go` is **append-only**: a released name can never be
renamed or edited, because a syncing node must replay the same handler at the same height.

### Building a release

Each release publishes **SHA-256 checksums** for its artifacts. Operators run cosmovisor with
`DAEMON_ALLOW_DOWNLOAD_BINARIES=false` and verify pre-staged binaries by hash, so the
checksum is the artifact that matters, not the download.

`make build` stamps the version and commit at link time and writes to
`build/twilightd`; `twilightd version --long` reports them. The chain and binary names are
compiled in, so even an unstamped `go build ./cmd/twilightd` identifies itself — an
unstamped build reports an empty version, which is honest, because it was not released.

`make build-release` produces the release artifacts:

```bash
make build-release VERSION=v0.1.0
```

Artifacts are built from **`git archive HEAD`**, not the working directory, so a release is the
commit it names by construction: no build variable reaches inside, and untracked files are
absent from the archive rather than merely undetected. The release is written to
`build/release/`:

- the three binaries, `twilightd-<version>-<os>-<arch>`;
- `LICENSE`, `NOTICE`, and `THIRD_PARTY_NOTICES`;
- `SHA256SUMS`, covering all six files. An operator checking a single binary runs
  `sha256sum -c --ignore-missing SHA256SUMS`.

Once the tag and its GitHub release exist, update the latest-release line on the
documentation site, which names the release in two places: `latestRelease` in
`website/src/pages/index.tsx` and the opening paragraph of `website/docs/intro.md`.

`THIRD_PARTY_NOTICES` is generated at release time by `scripts/third-party-notices.sh`, from
the same exported tree as the binaries. It carries the license, `NOTICE` and `PATENTS` files of
every module linked into `twilightd` (the union over the release targets) and of Go itself,
any `licenses/` directory at a module root, a `proxy.golang.org` source URL for each module,
and the list of MPL-2.0 modules (MPL-2.0 §3.2(a)). It is not committed. To inspect it, write it
under the git-ignored `build/` directory, so that it cannot trip the untracked-file refusal
below (the script does not create the directory):

```bash
mkdir -p build && scripts/third-party-notices.sh build/THIRD_PARTY_NOTICES
```

The release is assembled in a staging directory and swapped into place only after it verifies
against its own `SHA256SUMS`. It **refuses**, leaving any previous release byte-identical,
when:

- the tree has uncommitted changes to tracked files;
- there are untracked files that are not git-ignored, outside `docs/specs/` (the rule is
  default-deny: the toolchain consumes more than `.go` files, and `//go:embed` can reach a
  file of any extension);
- a `go.work` or `go.work.sum` exists (both are git-ignored, so they are checked by name);
- `LICENSE` or `NOTICE` is missing from `HEAD`;
- a linked module has no `LICENSE` or `COPYING` file at its root;
- `go.mod` or `go.sum` would change;
- any target fails to build, or the staged release does not verify against its own
  `SHA256SUMS`;
- `RELEASE_DIR` is not a normalised path under the git-ignored `build/` directory (the release
  directory is replaced wholesale, so it may only name a place whose loss costs nothing).

A signal ends the build in flight along with the script, and a run interrupted between setting
the previous release aside and moving the new one into place puts the previous release back.

The environment cannot change what is built. Releases run with `GOENV=off GOWORK=off
GOFLAGS=-mod=readonly CGO_ENABLED=0`, `GOAMD64=v1 GOARM64=v8.0 GOFIPS140=off`, an empty
`GOEXPERIMENT`, and `GOTOOLCHAIN` pinned to the `go` directive of the commit's `go.mod`, so the
Go version on the host does not change the binaries (go fetches the pinned toolchain if the
host's differs). Settings such as `GOPROXY` or `GOPRIVATE` must therefore be passed as real
environment variables, not through `go env -w`. A release works offline (`GOPROXY=off`) when
the module cache already holds what the build needs.

An artifact named for a version but built from uncommitted work would report a commit its
source does not match, and the checksum would hash it faithfully without disclosing that.
`make build` stays usable on a dirty tree and appends `-dirty` to whatever version it is given;
that marker cannot be switched off from the command line.

`make check-release-stamping` exercises the provenance guards against the real `Makefile`; it
needs a clean tree. It covers:

- `-dirty` stamping that a command-line `VERSION` or `DIRTY=` cannot remove;
- refusal of a dirty tree, untracked build inputs (`.go` and `.s`), and `go.work`, and that
  a refusal leaves an existing release in place;
- immunity to ambient `GOFLAGS` and to a user go env file;
- the `GOAMD64` and `GOEXPERIMENT` pins (the artifacts stay byte-identical);
- the staged swap: a failed target build, or `go.sum` changing mid-release, leaves the
  previous release byte-identical with no staging directory behind;
- a clean release producing three commit-stamped, `-trimpath`, `CGO_ENABLED=0` binaries listed
  in `SHA256SUMS`.

It does not exercise the license files and their checksums, the missing-module-license
refusal, the `RELEASE_DIR` guard, offline builds, or the `GOARM64` and `GOFIPS140` pins.

Binaries target the platforms validators actually run:

| target | why |
|---|---|
| `linux/amd64` | the dominant validator platform |
| `linux/arm64` | Graviton/Ampere validators |
| `darwin/arm64` | developer convenience; not for validators |

They build `CGO_ENABLED=0`, so the artifacts are static and the default `goleveldb` backend
is used. RocksDB is an indirect dependency and is not compiled in without its build tag.

## Review & quality gates

The `main` branch ruleset **enforces** that every change arrives through a pull request and
passes the six required CI checks — build & test, consensus vectors, `golangci-lint`, gofmt
& tidy, proto descriptor up to date, and `govulncheck` — before it can merge. See
[`REVIEW.md`](REVIEW.md) for what each check covers, the multi-model review pass we run
on changes, and the PR checklist. The one exception is a fix merged from a security
advisory's private fork, which bypasses CI and the ruleset; the maintainer runs the
CI-equivalent `make` targets listed in `REVIEW.md` locally before merging it, and CI runs
again on `main` afterwards.

Review of consensus-critical changes by a maintainer is a **process expectation**, not
something the ruleset enforces today: with a single maintainer ([`MAINTAINERS.md`](MAINTAINERS.md)),
required approvals are set to zero, because a sole maintainer cannot approve their own pull
request. Code-owner routing and required approvals will be added once there is more than one
maintainer.

## Determinism rules (important for a chain)

State-machine code must be **deterministic** across nodes:

- No `time.Now()`, `rand`, goroutines, or map-iteration-order dependence in any
  consensus path (BeginBlock/EndBlock/Msg handlers/keeper state transitions). Sort before
  iterating maps.
- Fail **closed**: on an unexpected condition in finalization, return an error so the
  block fails safely with **no partial state committed** — finalization runs in a cache
  context that is only written on full success — rather than committing inconsistent state.
- Never introduce a second source of `ValidatorUpdate`s — the validator set is owned
  exclusively by `x/coreslot`.

## License

Twilight Core is licensed under [Apache-2.0](LICENSE). Under section 5 of that license, unless
you explicitly state otherwise, any contribution you intentionally submit for inclusion is
licensed under the same terms, without any additional terms or conditions. No commit sign-off
or contributor license agreement is required.

## Code of Conduct

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md). By participating you are
expected to uphold it.
