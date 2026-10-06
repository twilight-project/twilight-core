#!/usr/bin/env bash
# Checks that a binary cannot claim to be something it is not.
#
# A release artifact asserts two things about its provenance: the version it was
# cut as, and the commit it was built from. Both are consumed by operators who
# pre-stage binaries and verify them by hash, so a stamp that is confidently
# wrong is worse than one that is missing — the checksum will hash the wrong
# artifact faithfully and disclose nothing.
#
# The case that shipped: dirtiness lived inside VERSION, so `make build
# VERSION=v0.1.0` on a modified tree replaced the whole `git describe --dirty`
# expression and dropped the marker. The binary then reported an exact commit
# its source did not match. Untested build logic is how that happened, so the
# cases below run against the real Makefile.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PASSED=0; FAILED=0
check() { # <name> <expected> <actual>
  if [[ "$2" == "$3" ]]; then printf '  ok    %-46s %s\n' "$1" "$2"; PASSED=$((PASSED+1))
  else printf '  FAIL  %-46s expected=%s actual=%s\n' "$1" "$2" "$3" >&2; FAILED=$((FAILED+1)); fi
}

PROBE="cmd/twilightd/main.go"
UNTRACKED_GO="cmd/twilightd/zz_provenance_probe.go"
UNTRACKED_ASM="cmd/twilightd/zz_provenance_probe.s"
GOWORK_PROBE="go.work"
RELEASE_DIR_PROBE="zz_release_dir_probe.untracked"
BIN="build/twilightd"
cleanup() { git checkout -- "$PROBE" 2>/dev/null || true; rm -f "$UNTRACKED_GO" "$UNTRACKED_ASM" "$RELEASE_DIR_PROBE" go.work go.work.sum; }
trap cleanup EXIT

# Refuse to run against a tree that is already modified: the cases below dirty a
# file deliberately and restore it, and doing that on top of real work would
# discard it.
#
# The guard uses the SAME default-deny rule it is testing. An earlier version
# checked tracked modifications only, so a probe file left behind by an
# interrupted run was still present when the next run started, and the two
# "clean tree" cases failed against a tree that was not clean. A test that can
# run against contaminated state reports on something other than what it claims.
if ! git diff-index --quiet HEAD -- 2>/dev/null; then
  echo "refusing to run: uncommitted changes to tracked files" >&2
  git --no-pager diff --stat HEAD -- >&2
  exit 2
fi
for w in go.work go.work.sum; do
  [[ -e "$w" ]] && { echo "refusing to run: $w is present" >&2; exit 2; }
done
LEFTOVER="$(git ls-files --others --exclude-standard -- . ':(exclude)docs/specs' 2>/dev/null)"
if [[ -n "$LEFTOVER" ]]; then
  echo "refusing to run: untracked files present (a previous run may have been interrupted)" >&2
  printf '%s\n' "$LEFTOVER" >&2
  exit 2
fi

stamped() { "$BIN" version --long 2>/dev/null | awk -v k="$1" -F': *' '$1==k {print $2}'; }

echo "=== a clean tree stamps exactly what it is asked to ==="
make build VERSION=v9.9.9 >/dev/null 2>&1
check "explicit version, clean"        "v9.9.9" "$(stamped version)"
check "commit is the committed HEAD"   "$(git rev-parse HEAD)" "$(stamped commit)"
make build >/dev/null 2>&1
check "default version has no -dirty"  "clean"  "$([[ "$(stamped version)" == *-dirty ]] && echo dirty || echo clean)"

echo
echo "=== untracked files are not modifications ==="
# docs/specs/ is untracked and permanently present here; if it counted as dirty,
# build-release could never run.
UNTRACKED_PROBE="$(mktemp -p . XXXXXX.untracked 2>/dev/null || mktemp ./XXXXXX.untracked)"
check "untracked does not mark dirty"  "clean" \
  "$(git diff-index --quiet HEAD -- 2>/dev/null && echo clean || echo dirty)"
rm -f "$UNTRACKED_PROBE"

echo
echo "=== a dirty tree cannot be hidden by an explicit version ==="
echo "// provenance probe" >>"$PROBE"
make build VERSION=v9.9.9 >/dev/null 2>&1
check "explicit version, dirty"        "v9.9.9-dirty" "$(stamped version)"
make build >/dev/null 2>&1
check "default version, dirty"         "dirty" \
  "$([[ "$(stamped version)" == *-dirty ]] && echo dirty || echo clean)"

echo
echo "=== provenance cannot be switched off from the command line ==="
# GNU Make lets a command-line assignment beat any assignment in the makefile, so
# `DIRTY=` blanked the marker and stamped a modified tree as clean.
make build VERSION=v9.9.9 DIRTY= >/dev/null 2>&1
check "DIRTY= cannot blank the marker"  "v9.9.9-dirty" "$(stamped version)"
make build-release VERSION=v9.9.9 DIRTY= >/dev/null 2>&1; rc=$?
check "DIRTY= cannot bypass the refusal" "nonzero" "$([[ $rc -ne 0 ]] && echo nonzero || echo zero)"

echo
echo "=== a release cannot be built from a dirty tree at all ==="
rm -rf build/release
make build-release VERSION=v9.9.9 >/dev/null 2>&1; rc=$?
# Non-zero is the property; the exact code is make's convention (2 for a failed
# recipe), and pinning it would be asserting make's internals rather than ours.
check "build-release exits non-zero"   "nonzero" "$([[ $rc -ne 0 ]] && echo nonzero || echo zero)"
# The guards run before anything is written, so a refusal produces no new
# artifacts. It also does not clear the directory: whatever was there survives,
# which the pre-existing-artifact case below asserts directly.
check "refusal produces no new artifacts" "absent" \
  "$([[ -d build/release ]] && echo present || echo absent)"

cleanup

echo
echo "=== untracked build inputs are not clean ==="
# diff-index sees tracked files only, so untracked sources compile into the binary
# while the tree reports clean. Enumerating extensions does not close this: the
# toolchain also consumes .s, .c, .h and .syso, and //go:embed reaches any
# extension at all. The rule is default-deny with docs/specs/ allowlisted.
printf 'package main\n' >"$UNTRACKED_GO"
check "untracked .go marks the tree dirty" "dirty" \
  "$(git diff-index --quiet HEAD -- 2>/dev/null \
      && test -z "$(git ls-files --others --exclude-standard -- '*.go' go.mod go.sum)" \
      && echo clean || echo dirty)"
make build VERSION=v9.9.9 >/dev/null 2>&1
check "untracked .go stamps -dirty"     "v9.9.9-dirty" "$(stamped version)"
make build-release VERSION=v9.9.9 >/dev/null 2>&1; rc=$?
check "untracked .go refuses a release" "nonzero" "$([[ $rc -ne 0 ]] && echo nonzero || echo zero)"
rm -f "$UNTRACKED_GO"

# Assembly is a compiler input a .go-only check missed; `go list` reports it under
# SFiles and it changes the binary.
printf '// provenance probe\n' >"$UNTRACKED_ASM"
check "untracked .s is a compiler input"  "1" "$(go list -f '{{len .SFiles}}' ./cmd/twilightd 2>/dev/null)"
check "untracked .s marks the tree dirty" "dirty" \
  "$(git diff-index --quiet HEAD -- 2>/dev/null \
      && test -z "$(git ls-files --others --exclude-standard -- . ':(exclude)docs/specs')" \
      && echo clean || echo dirty)"
make build VERSION=v9.9.9 >/dev/null 2>&1
check "untracked .s stamps -dirty"        "v9.9.9-dirty" "$(stamped version)"
make build-release VERSION=v9.9.9 >/dev/null 2>&1; rc=$?
check "untracked .s refuses a release"    "nonzero" "$([[ $rc -ne 0 ]] && echo nonzero || echo zero)"
rm -f "$UNTRACKED_ASM"

echo
echo "=== an ignored file that changes the build is still a modification ==="
# .gitignore lists go.work, and --exclude-standard skips ignored files, so the
# default-deny rule above is structurally blind to the one file that can redirect
# the whole module. It has to be checked by name.
printf 'go %s\n\nuse .\n' "$(go env GOVERSION | sed 's/^go//')" >"$GOWORK_PROBE"
check "go.work is invisible to default-deny" "0" \
  "$(git ls-files --others --exclude-standard -- . ':(exclude)docs/specs' | grep -c 'go.work')"
make build VERSION=v9.9.9 >/dev/null 2>&1
check "go.work stamps -dirty anyway"        "v9.9.9-dirty" "$(stamped version)"
make build-release VERSION=v9.9.9 >/dev/null 2>&1; rc=$?
check "go.work refuses a release"           "nonzero" "$([[ $rc -ne 0 ]] && echo nonzero || echo zero)"
# go build writes go.work.sum alongside it, and that file is checked by name too,
# so leaving it behind would make every later case run against a "dirty" tree.
rm -f go.work go.work.sum

echo
echo "=== ambient GOFLAGS cannot alter a release ==="
# GOFLAGS=-tags=upgradedrill compiles the drill upgrade handler in while BuildTags
# is stamped from our own variable and reports nothing.
rm -rf build/release
GOFLAGS=-tags=upgradedrill make build-release VERSION=v9.9.9 >/dev/null 2>&1
leaked=0
for a in build/release/twilightd-v9.9.9-*; do
  grep -aqF 'drill-v2' "$a" && leaked=$((leaked + 1))
done
check "no drill handler leaks into artifacts" "0" "$leaked"
rm -rf build/release

echo
echo "=== a user go env file cannot alter a release ==="
# go treats an empty GOFLAGS as unset and falls back to the user's go env file, so
# clearing the variable alone let `go env -w GOFLAGS=-tags=upgradedrill` compile
# the drill handler into every artifact while the stamp reported no tags. The
# probe file lives outside the tree so it cannot trip the untracked-file refusal.
GOENV_PROBE="$(mktemp -d)/env"
printf 'GOFLAGS=-tags=upgradedrill\n' >"$GOENV_PROBE"
# The premise, so the case below cannot pass vacuously: this file does reach go.
check "probe go env file sets GOFLAGS"      "-tags=upgradedrill" "$(GOENV="$GOENV_PROBE" GOFLAGS= go env GOFLAGS)"
GOENV="$GOENV_PROBE" make build-release VERSION=v9.9.9 >/dev/null 2>&1; rc=$?
check "release builds under the probe"      "0" "$rc"
check "three artifacts under the probe"     "3" "$(ls build/release/twilightd-v9.9.9-* 2>/dev/null | wc -l | tr -d ' ')"
# "Adds no build tags" passes on ZERO matches, so anything that loses a match
# passes it for the wrong reason: a build info that was not read, or read only in
# part, or a matcher that never ran. So the build info is read into a variable
# and matched inside bash, with no pipe and no here-string between the two (a
# `go version -m … | grep -q` under pipefail reports a match as a miss once the
# output outgrows a pipe; a here-string needs a temp file that can fail to be
# created). And "was read" means go exited 0 AND the output reaches the GOOS
# build setting, which sorts after -tags: an artifact that is missing, not a Go
# binary, or cut short is counted as unread and failed, not taken as "no tags"
# (#222).
leaked=0; tagged=0; unread=0
for a in build/release/twilightd-v9.9.9-*; do
  grep -aqF 'drill-v2' "$a" 2>/dev/null && leaked=$((leaked + 1))
  if info="$(go version -m "$a" 2>/dev/null)" && [[ "$info" == *$'\tbuild\tGOOS='* ]]; then
    [[ "$info" == *-tags=* ]] && tagged=$((tagged + 1))
  else
    unread=$((unread + 1))
  fi
done
check "go env file leaks no drill handler"  "0" "$leaked"
check "every artifact's build info was read" "0" "$unread"
check "go env file adds no build tags"      "0" "$tagged"
rm -rf "$(dirname "$GOENV_PROBE")" build/release

echo
echo "=== a refusal preserves artifacts that were already there ==="
mkdir -p build/release && echo sentinel >build/release/PREEXISTING
echo "// provenance probe" >>"$PROBE"
make build-release VERSION=v9.9.9 >/dev/null 2>&1
check "pre-existing artifacts survive"  "present" \
  "$([[ -f build/release/PREEXISTING ]] && echo present || echo absent)"
cleanup; rm -rf build/release

echo
echo "=== a release that fails partway leaves the previous release untouched ==="
# The release is staged and swapped in only once complete. Before that, a build
# failing on the second target, or go.sum changing under the build, left the
# release directory wiped and holding officially named binaries with no
# SHA256SUMS. A stand-in go on PATH forces each failure after real work is done;
# every other go invocation passes through to the real toolchain.
REAL_GO="$(command -v go)"
SHIM="$(mktemp -d)"
cat >"$SHIM/go" <<EOF
#!/usr/bin/env bash
if [[ "\$1" == build && "\${GOOS:-}/\${GOARCH:-}" == "\${SHIM_TARGET:-}" ]]; then
  case "\${SHIM_MODE:-}" in
    fail)   echo "forced build failure" >&2; exit 1 ;;
    tamper) "$REAL_GO" "\$@" || exit
            echo "example.com/tamper v0.0.0/go.mod h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" >>go.sum
            exit 0 ;;
  esac
fi
exec "$REAL_GO" "\$@"
EOF
chmod +x "$SHIM/go"
release_snapshot() { ( cd build/release 2>/dev/null && ls -A | LC_ALL=C sort && cksum -- * ); }
leftovers() { find build -maxdepth 1 \( -name '.release-staging.*' -o -name '.release-old.*' \) 2>/dev/null | wc -l | tr -d ' '; }
for spec in "fail linux/arm64" "tamper darwin/arm64"; do
  read -r mode target <<<"$spec"
  mkdir -p build/release
  echo "previous binary" >build/release/twilightd-v0.0.1-linux-amd64
  ( cd build/release && cksum twilightd-v0.0.1-linux-amd64 >SHA256SUMS )
  before="$(release_snapshot)"
  touch "$SHIM/marker"
  PATH="$SHIM:$PATH" SHIM_MODE="$mode" SHIM_TARGET="$target" make build-release VERSION=v9.9.9 >/dev/null 2>&1; rc=$?
  check "$mode on $target refuses"               "nonzero" "$([[ $rc -ne 0 ]] && echo nonzero || echo zero)"
  check "$mode: previous release byte-identical" "same" "$([[ "$(release_snapshot)" == "$before" ]] && echo same || echo changed)"
  check "$mode: nothing in it rewritten"         "0" "$(find build/release -newer "$SHIM/marker" | wc -l | tr -d ' ')"
  check "$mode: no staging left behind"          "0" "$(leftovers)"
  rm -rf build/release
done
rm -rf "$SHIM"

echo
echo "=== and succeeds once the tree is clean again ==="
make build-release VERSION=v9.9.9 >/dev/null 2>&1; rc=$?
check "build-release exits zero"       "0" "$rc"
check "three artifacts"                "3" "$(ls build/release/twilightd-* 2>/dev/null | wc -l | tr -d ' ')"
check "named with the clean version"   "3" "$(ls build/release/twilightd-v9.9.9-* 2>/dev/null | wc -l | tr -d ' ')"
check "checksums cover every artifact" "3" "$(grep -c 'twilightd-' build/release/SHA256SUMS 2>/dev/null || echo 0)"
# Every artifact, not just the host-native one, and without executing any of them:
# a Linux validator or CI runner cannot exec the darwin build, and picking the
# host's target would leave the other two unverified.
HEAD_SHA="$(git rev-parse HEAD)"
stamped_all=0
for a in build/release/twilightd-v9.9.9-*; do
  grep -aqF "$HEAD_SHA" "$a" && stamped_all=$((stamped_all + 1))
done
check "every artifact carries the commit" "3" "$stamped_all"
flags_all=0
for a in build/release/twilightd-v9.9.9-*; do
  info="$(go version -m "$a" 2>/dev/null || true)"
  [[ "$info" == *trimpath=true* && "$info" == *CGO_ENABLED=0* ]] \
    && flags_all=$((flags_all + 1))
done
check "every artifact is trimpath+CGO0"   "3" "$flags_all"

echo
echo "=== code-generation variables cannot alter a release ==="
# GOAMD64=v3 produced a different linux/amd64 binary under the same release name,
# and GOEXPERIMENT carried through the same way. The release pins both, so the
# artifacts must be byte-identical to the clean run above.
CLEAN_SUMS="$(cat build/release/SHA256SUMS 2>/dev/null)"
check "probe GOAMD64 reaches go"          "v3"         "$(GOAMD64=v3 go env GOAMD64)"
check "probe GOEXPERIMENT is valid here"  "greenteagc" "$(GOEXPERIMENT=greenteagc go env GOEXPERIMENT 2>/dev/null)"
rm -rf build/release
GOAMD64=v3 GOEXPERIMENT=greenteagc make build-release VERSION=v9.9.9 >/dev/null 2>&1; rc=$?
check "release builds under the probe"    "0" "$rc"
check "artifacts identical to clean run"  "same" \
  "$([[ -n "$CLEAN_SUMS" && "$(cat build/release/SHA256SUMS 2>/dev/null)" == "$CLEAN_SUMS" ]] && echo same || echo different)"
rm -rf build/release

echo
echo "=== RELEASE_DIR may only name a place under build/ ==="
# The release directory is replaced wholesale. `.git`, `docs`, `x` and `app` used
# to pass the check, and a run with one of them replaced that directory with
# release files; `.//` and `./.` passed and failed only at the swap, after a full
# build. Each must be refused before any work.
#
# These runs must not be able to reach the swap even if the rule under test is
# broken, or a regression would have this suite replace the repository's own .git.
# So an untracked probe file is present throughout: the untracked-file guard,
# which runs AFTER the RELEASE_DIR rule and is proven above, refuses anything the
# rule lets through. A run is counted only if it was refused BY the rule.
: >"$RELEASE_DIR_PROBE"
ERR="$(mktemp)"
refused=0; total=0
# The last one carries a newline: a component check that stops at a line end
# never sees the `..` after it, and git normalises `..` when deciding what is
# ignored, so it resolved to an ignored directory outside build/.
for bad in .git docs x app . ./ .// ./. build build/ build/. build/./release build/../docs build//release /tmp/release ../release $'build/x\n/../../docs'; do
  total=$((total + 1))
  RELEASE_DIR="$bad" RELEASE_TARGETS=linux/amd64 VERSION=v9.9.9 ./scripts/build-release.sh >/dev/null 2>"$ERR"; rc=$?
  if [[ $rc -ne 0 ]] && grep -q 'RELEASE_DIR' "$ERR"; then
    refused=$((refused + 1))
  else
    echo "    not refused by the RELEASE_DIR rule: '$bad'" >&2
  fi
done
check "every unsafe RELEASE_DIR is refused"   "$total" "$refused"
# The old rule refused any name containing two dots. This one is only a name.
RELEASE_DIR='build/v1..2' RELEASE_TARGETS=linux/amd64 VERSION=v9.9.9 ./scripts/build-release.sh >/dev/null 2>"$ERR"; rc=$?
check "build/v1..2 passes the RELEASE_DIR rule" "stopped by the next guard" \
  "$(if grep -q 'RELEASE_DIR' "$ERR"; then echo "refused by the rule"
     elif [[ $rc -ne 0 ]] && grep -q 'untracked files present' "$ERR"; then echo "stopped by the next guard"
     else echo "not stopped (exit $rc)"; fi)"
check "the repository is intact"              "$HEAD_SHA" "$(git rev-parse HEAD 2>/dev/null)"
check "no release directory was created"      "absent" \
  "$([[ -e build/release || -e 'build/v1..2' ]] && echo present || echo absent)"
rm -f "$RELEASE_DIR_PROBE" "$ERR"

echo
echo "=== a signal cannot cost a release, or leave a stray one ==="
# Two windows, each reproduced with a stand-in on PATH that signals the release
# script alone, as `kill <pid>` does (a terminal's Ctrl-C reaches the whole
# process group and never showed either).
#
#   between the two swap renames: the previous release had been set aside and
#     the EXIT cleanup deleted it together with the staged one, so a TERM in a
#     window of milliseconds left no release at all.
#   during a build: the script cleaned up and exited while `go build` ran on,
#     recreated the staging directory (`go build -o` creates parents) and left an
#     officially named binary in it.
#
# The same run records the toolchain every go invocation saw, under an ambient
# GOTOOLCHAIN the release must override.
REAL_GO="$(command -v go)"; REAL_MV="$(command -v mv)"
SIG="$(mktemp -d)"
cat >"$SIG/go" <<STANDIN
#!/usr/bin/env bash
printf '%s\n' "\${GOTOOLCHAIN-<unset>}" >>"$SIG/toolchain.seen"
if [[ "\$1" == build && "\${SHIM_MODE:-}" == orphan ]]; then
  out=""; prev=""
  for a in "\$@"; do [[ "\$prev" == -o ]] && out="\$a"; prev="\$a"; done
  kill -TERM "\$(cat "$SIG/script.pid")"
  sleep 4   # finish late, the way an orphaned build does
  mkdir -p "\$(dirname "\$out")" && echo "orphaned build output" >"\$out"
  exit 0
fi
exec "$REAL_GO" "\$@"
STANDIN
cat >"$SIG/mv" <<STANDIN
#!/usr/bin/env bash
"$REAL_MV" "\$@"; rc=\$?
# The rename that sets the previous release aside is the one into
# .release-old.*/release. Signal the release script once it has happened.
if [[ "\${SHIM_MODE:-}" == midswap && "\${!#}" == */.release-old.*/release ]]; then
  kill -TERM "\$(cat "$SIG/script.pid")"
fi
exit \$rc
STANDIN
chmod +x "$SIG/go" "$SIG/mv"
run_signalled() { # <mode> -> the release script's exit code
  PATH="$SIG:$PATH" SHIM_MODE="$1" GOTOOLCHAIN=local RELEASE_TARGETS=linux/amd64 VERSION=v9.9.9 \
    ./scripts/build-release.sh >/dev/null 2>&1 &
  local pid=$!
  echo "$pid" >"$SIG/script.pid"
  wait "$pid"
}
previous_release() {
  mkdir -p build/release
  echo "previous binary" >build/release/twilightd-v0.0.1-linux-amd64
  ( cd build/release && cksum twilightd-v0.0.1-linux-amd64 >SHA256SUMS )
}

previous_release; before="$(release_snapshot)"
: >"$SIG/toolchain.seen"
run_signalled midswap; rc=$?
check "TERM between the two renames exits 143"    "143"  "$rc"
check "midswap: previous release is back, intact" "same" "$([[ "$(release_snapshot)" == "$before" ]] && echo same || echo changed)"
check "midswap: nothing set aside or staged left" "0"    "$(leftovers)"

# That run built a whole target before it was signalled, under GOTOOLCHAIN=local.
PINNED="go$(awk '/^go [0-9]/ { print $2; exit }' go.mod)"
check "probe: an ambient GOTOOLCHAIN reaches go"  "local" "$(GOTOOLCHAIN=local go env GOTOOLCHAIN)"
check "go was invoked during the release"         "yes"   "$([[ -s "$SIG/toolchain.seen" ]] && echo yes || echo no)"
check "every invocation used go.mod's toolchain"  "$PINNED" "$(LC_ALL=C sort -u "$SIG/toolchain.seen" | tr '\n' ' ' | sed 's/ $//')"
rm -rf build/release

# The notices script pins the toolchain itself, for stand-alone runs. Inside a
# release it inherits the pin, so only a run on its own can show that its own
# pin works; the output goes under build/ where it cannot trip the untracked guard.
: >"$SIG/toolchain.seen"
mkdir -p build
PATH="$SIG:$PATH" GOTOOLCHAIN=local RELEASE_TARGETS=linux/amd64 ./scripts/third-party-notices.sh build/THIRD_PARTY_NOTICES.probe >/dev/null 2>&1; rc=$?
check "notices run stand-alone under the probe"   "0"     "$rc"
check "stand-alone notices used go.mod's toolchain" "$PINNED" "$(LC_ALL=C sort -u "$SIG/toolchain.seen" | tr '\n' ' ' | sed 's/ $//')"
rm -f build/THIRD_PARTY_NOTICES.probe

previous_release; before="$(release_snapshot)"
run_signalled orphan; rc=$?
check "TERM during a build exits 143"             "143"  "$rc"
sleep 6   # longer than the stand-in's delay: an orphaned build would have finished
check "orphan: the build ended with the script"   "0"    "$(leftovers)"
check "orphan: previous release untouched"        "same" "$([[ "$(release_snapshot)" == "$before" ]] && echo same || echo changed)"
rm -rf build/release "$SIG"

make build >/dev/null 2>&1   # leave a normally-stamped binary behind

echo
if (( FAILED > 0 )); then
  echo "release stamping: FAIL ($FAILED of $((PASSED+FAILED)))" >&2; exit 1
fi
echo "release stamping: PASS ($PASSED checks)"
