#!/usr/bin/env bash
# Builds release artifacts from the committed tree, and refuses anything else.
#
# A release artifact asserts it is a particular version built from a particular
# commit. Operators pre-stage binaries with cosmovisor's downloader disabled and
# verify them by hash, so a stamp that is confidently wrong is worse than one
# that is missing: the checksum hashes the artifact faithfully and cannot
# disclose that the source differed from the commit named on it.
#
# Two ways that happened while this was a Makefile recipe:
#
#   - dirtiness was a Make variable, and GNU Make lets a command-line assignment
#     override any assignment in the makefile. `make build-release DIRTY=` blanked
#     the guard and produced officially named artifacts from a modified tree.
#   - the guard consulted `git diff-index`, which sees tracked files only, and a
#     follow-up that enumerated .go/go.mod/go.sum still missed .s — the toolchain
#     also consumes .s, .c, .h and .syso, and //go:embed reaches any extension.
#     Untracked build inputs compiled into the binary while the tree reported clean.
#
# Both existed because the release was built from the mutable worktree. This
# builds from `git archive HEAD` instead, so the artifact is the commit it claims
# by construction: no Make variable reaches inside, and untracked files are absent
# from the archive rather than merely undetected. The guards below remain, because
# refusing early with a clear reason beats silently building something different
# from what the operator is looking at.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

RELEASE_DIR="${RELEASE_DIR:-build/release}"
TARGETS="${RELEASE_TARGETS:-linux/amd64 linux/arm64 darwin/arm64}"

refuse() { echo "refusing to build a release: $1" >&2; shift; [[ $# -gt 0 ]] && printf '%s\n' "$@" >&2; exit 1; }

command -v git >/dev/null 2>&1 || refuse "git is required to establish provenance"
git rev-parse HEAD >/dev/null 2>&1 || refuse "not a git repository, so the commit cannot be established"

# The release directory is replaced wholesale at the end, so it may only name a
# place whose loss costs nothing: a normalised path under build/, which git
# ignores. A looser rule ("relative, below the repository") accepted `.git`,
# `docs`, `x` and `app`, and replaced the named directory with release files; it
# also let `.//` and `./.` through to fail at the swap, after a full build, and
# refused a legitimate name such as build/v1..2 for containing two dots.
#
# Each component is checked, not the string: `..` as a component climbs out,
# while `v1..2` is only a name. A control character is refused outright: `read`
# below stops at a newline, so a value with `..` components after one would be
# checked only up to the newline, and git normalises `..` when deciding whether
# a path is ignored. The ignore check is the backstop for the rule itself: if
# build/ ever stopped being ignored, a release directory there would be tracked
# content again.
release_dir_ok() {
  local dir="$1" part parts
  [[ "$dir" == build/* && "$dir" != */ && "$dir" != *[[:cntrl:]]* ]] || return 1
  # read -a, not an unquoted expansion: a component such as `*` must stay a
  # name here and not expand to whatever is in the working directory.
  IFS=/ read -r -a parts <<<"$dir"
  for part in "${parts[@]}"; do
    [[ -n "$part" && "$part" != . && "$part" != .. ]] || return 1
  done
  return 0
}
release_dir_ok "$RELEASE_DIR" \
  || refuse "RELEASE_DIR must be a normalised path under build/ (no empty, '.' or '..' components): '$RELEASE_DIR'"
git check-ignore -q -- "$RELEASE_DIR" \
  || refuse "RELEASE_DIR is not ignored by git, so replacing it could destroy tracked content: '$RELEASE_DIR'"

# --- guards, evaluated here rather than as Make variables so no caller can blank
# --- them from the command line.
if ! git diff-index --quiet HEAD -- 2>/dev/null; then
  refuse "uncommitted changes to tracked files" "$(git --no-pager diff --stat HEAD --)"
fi

# Any untracked file outside the allowlist. Enumerating build-relevant extensions
# does not close: the toolchain also consumes .s, .c, .h and .syso, and //go:embed
# can pull in a file of any extension. So this is default-deny, and the allowlist
# names what is known safe rather than guessing what is dangerous.
#
# docs/specs/ is the one entry — user-owned material this project keeps untracked
# by convention, which the compiler cannot reach. Everything gitignored, build/
# included, is already excluded by --exclude-standard.
UNTRACKED="$(git ls-files --others --exclude-standard -- . ':(exclude)docs/specs' 2>/dev/null)"
if [[ -n "$UNTRACKED" ]]; then
  refuse "untracked files present; a release is built only from a clean tree" "$UNTRACKED"
fi

# .gitignore lists go.work/go.work.sum, and --exclude-standard skips ignored
# files, so the rule above cannot see the one file that can redirect the module.
for w in go.work go.work.sum; do
  [[ -e "$w" ]] && refuse "$w is present and would change module resolution"
done

# Ambient GOFLAGS can inject build tags: `GOFLAGS=-tags=upgradedrill` compiles the
# drill upgrade handler into the binary while BuildTags is stamped from our own
# variable and reports nothing. A release must not depend on the environment it
# happened to be cut from.
#
# Clearing the variable is not enough on its own: go treats an empty GOFLAGS as
# unset and falls back to the user's go env file (`go env -w GOFLAGS=...`), which
# injected the same tags. GOENV=off stops that file being read at all; GOROOT's
# own go.env defaults (proxy, checksum database, toolchain) still apply, and
# anything an operator genuinely needs can still be passed as a real environment
# variable. The flags are set explicitly, not left to defaults, and the
# third-party-notices step sets the same ones, so it lists exactly the module set
# this build links.
export GOENV=off GOWORK=off GOFLAGS=-mod=readonly

# The same holds for the variables that select code generation rather than flags:
# `GOAMD64=v3` or a GOEXPERIMENT in the environment produced a different binary
# under the same release name. They are pinned to the toolchain defaults (the
# values an unset environment gives) and the per-architecture ones that do not
# apply to the release targets are cleared, so the artifact depends on the commit
# and the toolchain only. third-party-notices.sh pins the same values.
export GOAMD64=v1 GOARM64=v8.0 GOEXPERIMENT= GOFIPS140=off CGO_ENABLED=0
unset GOARM GO386 GOMIPS GOMIPS64 GOPPC64 GORISCV64 GOWASM

COMMIT="$(git rev-parse HEAD)"
VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo unknown)}"
BUILD_TAGS="${BUILD_TAGS:-}"

# Source comes from the commit, not the working directory.
SRC="$(mktemp -d)"
META="$(mktemp -d)"
OUT="$ROOT/$RELEASE_DIR"
STAGE=""; OLD=""; SWAPPED=0; CHILD=""

# cleanup never deletes the only copy of the previous release.
#
# The swap at the end is two renames: the previous release is set aside into
# $OLD, then the staged one is moved into place. A run that ends between them,
# on a signal say, has a previous release in $OLD and nothing at $OUT. It is put
# back. $OLD is removed only once the swap is known complete; if the run ended
# after the second rename but before that was recorded, both are left where they
# are, because a leftover directory costs disk space and a wrong guess costs a
# release.
cleanup() {
  # A second signal must not interrupt the restore, and the restore comes
  # before anything slow: a TERM during the removal of the exported source
  # would otherwise leave the previous release stranded under $OLD unannounced.
  trap '' INT TERM HUP
  if [[ -n "$OLD" && -e "$OLD/release" ]]; then
    if (( SWAPPED )); then
      rm -rf "$OLD"
    elif [[ ! -e "$OUT" ]] && mv "$OLD/release" "$OUT" 2>/dev/null; then
      rm -rf "$OLD"
    else
      echo "the previous release is kept at $OLD/release" >&2
    fi
  elif [[ -n "$OLD" ]]; then
    rm -rf "$OLD"
  fi
  [[ -n "$STAGE" ]] && rm -rf "$STAGE"
  rm -rf "$SRC" "$META"
  return 0
}
trap cleanup EXIT

# A signal ends the build as well as the script. Without this, a TERM delivered
# to the script alone left `go build` running: it finished after cleanup had
# removed the staging directory, recreated it (`go build -o` creates parents),
# and left an officially named binary there. The step in flight runs in its own
# process group (see in_group) so that the whole of it can be ended first.
on_signal() { # <exit code>
  trap - INT TERM HUP
  if [[ -n "$CHILD" ]]; then
    kill -TERM -- "-$CHILD" 2>/dev/null || kill -TERM "$CHILD" 2>/dev/null || true
    # A stopped job cannot act on the TERM. Being in a background group, the
    # step is stopped if it touched the terminal while `stty tostop` was set.
    kill -CONT -- "-$CHILD" 2>/dev/null || kill -CONT "$CHILD" 2>/dev/null || true
    wait "$CHILD" 2>/dev/null || true
    CHILD=""
  fi
  exit "$1"
}
trap 'on_signal 130' INT
trap 'on_signal 143' TERM
trap 'on_signal 129' HUP

# in_group runs a step as a background job with job control on, which gives it a
# process group of its own, and waits for it. The group is what on_signal ends:
# the toolchain runs compilers and a linker as children, and ending only the
# direct child would leave those writing into the staging directory.
#
# Standard input is detached, since a background group that reads the terminal
# is stopped rather than served. Writing to the terminal is still allowed by
# default; a terminal with `stty tostop` set stops the step at its first write,
# and the run waits until it is continued or ended (on_signal continues it).
in_group() {
  local rc
  set -m
  ( "$@" ) </dev/null &
  CHILD=$!
  set +m
  wait "$CHILD"; rc=$?
  CHILD=""
  return "$rc"
}

# Staging directories older than an hour are from a run that was killed outright
# (SIGKILL runs no cleanup). They are never the only copy of anything: a staged
# release that reached the swap is no longer under this name. Directories holding
# a set-aside previous release (.release-old.*) are deliberately not touched.
find "$(dirname "$OUT")" -maxdepth 1 -type d -name '.release-staging.*' -mmin +60 \
  -exec rm -rf {} + 2>/dev/null || true

git archive HEAD | tar -x -C "$SRC" || refuse "could not export HEAD"

# The toolchain is pinned to the commit's own go directive. Under the default
# GOTOOLCHAIN=auto the build uses the newer of the host's go and go.mod's, so
# two maintainers with different local Go versions produced different binaries
# for the same tag. It is read from the exported go.mod, like everything else a
# release is built from, and third-party-notices.sh inherits it. (A `toolchain`
# line in go.mod would not pin anything under auto, and `go mod tidy` removes it
# when it equals the go directive.)
GO_VERSION="$(awk '/^go [0-9]/ { print $2; exit }' "$SRC/go.mod")"
[[ "$GO_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(rc[0-9]+)?$ ]] \
  || refuse "the committed go.mod must name a full toolchain version in its go directive (1.N.P), to pin the build to one; found '$GO_VERSION'"
export GOTOOLCHAIN="go$GO_VERSION"

# The binaries statically link third-party modules whose licenses must travel with
# them (Apache-2.0 §4(d) also requires CometBFT's NOTICE), so every release carries
# LICENSE, NOTICE and a third-party bundle. All three come from the same exported
# commit as the binaries, and the bundle is generated before anything is written:
# a linked module with no license file refuses the release like any other guard.
for f in LICENSE NOTICE; do
  [[ -f "$SRC/$f" ]] || refuse "$f is missing from HEAD"
done
# The exported go.mod/go.sum are the commit's. Nothing after this point may
# change them: a step that filled in a missing go.sum entry would let a release
# build from a commit whose own go.sum could not build it.
cp "$SRC/go.mod" "$META/go.mod.committed" && cp "$SRC/go.sum" "$META/go.sum.committed" \
  || refuse "could not record the committed go.mod/go.sum"
module_files_unchanged() {
  cmp -s "$SRC/go.mod" "$META/go.mod.committed" && cmp -s "$SRC/go.sum" "$META/go.sum.committed"
}
notices() { cd "$SRC" && RELEASE_TARGETS="$TARGETS" bash ./scripts/third-party-notices.sh "$META/THIRD_PARTY_NOTICES"; }
in_group notices || refuse "could not produce the third-party notices"
module_files_unchanged || refuse "go.mod or go.sum changed while producing the third-party notices"

LDFLAGS="-X github.com/cosmos/cosmos-sdk/version.Version=$VERSION \
-X github.com/cosmos/cosmos-sdk/version.Commit=$COMMIT \
-X github.com/cosmos/cosmos-sdk/version.BuildTags=$BUILD_TAGS"

# The whole release is assembled in a staging directory beside the release
# directory and swapped in only once every file exists and every check has
# passed. A refusal at any point — a failed build partway through the targets, or
# go.mod/go.sum changing under the build — therefore leaves the previous release
# exactly as it was, instead of a wiped directory holding officially named
# binaries and no SHA256SUMS. Staging on the same filesystem makes the swap two
# renames rather than a copy.
PARENT="$(dirname "$OUT")"
mkdir -p "$PARENT" || refuse "could not create $(dirname "$RELEASE_DIR")"
STAGE="$(mktemp -d "$PARENT/.release-staging.XXXXXX")" || refuse "could not create a staging directory"
# mktemp -d creates 0700; give the release the mode a plain mkdir would.
chmod "$(printf '%o' $(( 0777 & ~$(umask) )))" "$STAGE" || refuse "could not set the staging directory mode"

for t in $TARGETS; do
  os="${t%%/*}"; arch="${t##*/}"
  name="twilightd-$VERSION-$os-$arch"
  echo "  building $RELEASE_DIR/$name  (from $COMMIT)"
  build_target() {
    cd "$SRC" && GOOS="$os" GOARCH="$arch" \
      go build -trimpath -ldflags "$LDFLAGS" -o "$STAGE/$name" ./cmd/twilightd
  }
  in_group build_target || refuse "build failed for $t"
done
module_files_unchanged || refuse "go.mod or go.sum changed during the build"

cp "$SRC/LICENSE" "$SRC/NOTICE" "$META/THIRD_PARTY_NOTICES" "$STAGE/" \
  || refuse "could not copy the license files"

# The license files are checksummed alongside the binaries: they are part of the
# release, and an operator verifying SHA256SUMS verifies them too.
( cd "$STAGE" && { command -v sha256sum >/dev/null \
                     && sha256sum twilightd-* LICENSE NOTICE THIRD_PARTY_NOTICES \
                   || shasum -a 256 twilightd-* LICENSE NOTICE THIRD_PARTY_NOTICES; } > SHA256SUMS ) \
  || refuse "could not write checksums"
( cd "$STAGE" && { command -v sha256sum >/dev/null && sha256sum -c --quiet SHA256SUMS \
                   || shasum -a 256 -c --quiet SHA256SUMS; } ) >/dev/null \
  || refuse "the staged release does not verify against its own SHA256SUMS"

# The swap. Everything above is complete and checked; the old release is renamed
# aside, the staged one renamed into place, and the old one removed only after
# that succeeded. If the second rename fails the old release is put back.
if [[ -e "$OUT" ]]; then
  OLD="$(mktemp -d "$PARENT/.release-old.XXXXXX")" || refuse "could not set the previous release aside"
  mv "$OUT" "$OLD/release" || refuse "could not set the previous release aside"
fi
if ! mv "$STAGE" "$OUT"; then
  if [[ -n "$OLD" ]] && ! mv "$OLD/release" "$OUT"; then
    kept="$OLD/release"; OLD=""   # never delete the only copy of the previous release
    refuse "could not move the staged release into $RELEASE_DIR" "the previous release is kept at $kept"
  fi
  refuse "could not move the staged release into $RELEASE_DIR"
fi
SWAPPED=1
STAGE=""

echo
echo "  $RELEASE_DIR/SHA256SUMS"
cat "$OUT/SHA256SUMS"
