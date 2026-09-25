#!/usr/bin/env bash
# Writes the third-party license and notice bundle for the twilightd release binaries.
#
# A release binary statically links every third-party Go module it imports, plus the
# Go standard library and runtime. Their licenses travel with the binary: BSD and MIT
# require the copyright and license text in binary redistributions, and Apache-2.0
# §4(d) requires a component's NOTICE file (CometBFT ships one) to be carried too.
# Binaries alone do not do that, so each release also ships the bundle this writes.
#
# What is covered is the set of modules ACTUALLY LINKED into ./cmd/twilightd — the
# union over the release targets, with the release's CGO_ENABLED=0 — not the whole
# go.mod graph, which also holds test-only and tool modules that never reach a
# binary. For each linked package the license files are collected from its own
# directory up to its module root, so a vendored subtree carrying its own license
# is included when (and only when) it is linked.
#
# The output is deterministic: same source tree, toolchain and targets give a
# byte-identical file. Nothing host-specific (module cache path, time, user) is
# written, modules are ordered by path and files by name in the C locale, and the
# content is read from the module cache, whose files are go.sum-verified. The
# only network access is `go mod download`; after it, listing runs with
# GOPROXY=off so a missing module fails instead of being fetched.
#
# Exits non-zero, writing nothing, if any linked module has no license file: a
# release must not ship a component whose terms it cannot state.
#
# Usage: scripts/third-party-notices.sh [OUTPUT]      (default: stdout)
#   RELEASE_TARGETS  space-separated GOOS/GOARCH list
#                    (default: linux/amd64 linux/arm64 darwin/arm64, as build-release.sh)
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OUTPUT="${1:-}"
TARGETS="${RELEASE_TARGETS:-linux/amd64 linux/arm64 darwin/arm64}"
PKG="./cmd/twilightd"

export LC_ALL=C
# The environment must not change what is listed: a go.work could redirect a
# module, and ambient GOFLAGS could add build tags that link something else.
export GOWORK=off GOFLAGS=-mod=readonly CGO_ENABLED=0

die() { echo "third-party-notices: $*" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

go mod download || die "go mod download failed"
export GOPROXY=off

# One line per linked non-main package:
#   module-path <TAB> version <TAB> module-dir <TAB> package-dir
# A replaced module is reported under its original path with "=> replacement"
# as its version, and its files are read from the replacement.
TEMPLATE='{{if .Module}}{{if not .Module.Main}}{{.Module.Path}}	{{with .Module.Replace}}=> {{.Path}}{{with .Version}}@{{.}}{{end}}	{{.Dir}}{{else}}{{.Module.Version}}	{{.Module.Dir}}{{end}}	{{.Dir}}
{{end}}{{end}}'
for t in $TARGETS; do
  GOOS="${t%%/*}" GOARCH="${t##*/}" go list -deps -f "$TEMPLATE" "$PKG" >>"$WORK/pkgs" \
    || die "go list failed for $t"
done
sort -u "$WORK/pkgs" -o "$WORK/pkgs"
[[ -s "$WORK/pkgs" ]] || die "no third-party packages listed; refusing to write an empty bundle"

# The directories to search: each package directory and every ancestor up to and
# including its module root, as paths relative to that root ("." for the root).
while IFS=$'\t' read -r mod ver moddir pkgdir; do
  [[ -n "$moddir" && -d "$moddir" ]] || die "$mod $ver is not in the module cache"
  rel="${pkgdir#"$moddir"}"; rel="${rel#/}"
  while :; do
    printf '%s\t%s\t%s\t%s\n' "$mod" "$ver" "$moddir" "${rel:-.}"
    [[ -z "$rel" ]] && break
    case "$rel" in */*) rel="${rel%/*}" ;; *) rel="" ;; esac
  done
done <"$WORK/pkgs" | sort -u >"$WORK/dirs"

# License-like file names. Anchored and extension-limited so license.go or
# notice_test.go never match, while LICENSE, LICENSE.md, LICENSE-APACHE,
# COPYING.txt, NOTICE and Go's PATENTS grant do.
# Matching is case-insensitive (nocasematch), so "license" and "Notice.txt" count.
shopt -s nocasematch
is_license_name() {
  [[ "$1" =~ ^(LICEN[CS]E|COPYING|NOTICE|PATENTS)([._-][A-Z0-9._-]*)?$ ]] || return 1
  [[ "$1" =~ \.(GO|S|C|H|PROTO|JSON|YAML|YML|SH|PY|TMPL|HTML|GOLDEN)$ ]] && return 1
  return 0
}

# module <TAB> version <TAB> absolute-file <TAB> file-relative-to-module
: >"$WORK/files"
while IFS=$'\t' read -r mod ver moddir rel; do
  dir="$moddir"; [[ "$rel" != "." ]] && dir="$moddir/$rel"
  for f in "$dir"/* "$dir"/.[!.]*; do
    [[ -f "$f" ]] || continue
    b="${f##*/}"
    is_license_name "$b" || continue
    r="$b"; [[ "$rel" != "." ]] && r="$rel/$b"
    printf '%s\t%s\t%s\t%s\n' "$mod" "$ver" "$f" "$r" >>"$WORK/files"
  done
done <"$WORK/dirs"
sort -t $'\t' -k1,1 -k4,4 -u "$WORK/files" -o "$WORK/files"

cut -f1,2 "$WORK/pkgs" | sort -u >"$WORK/modules"
MISSING="$(cut -f1 "$WORK/files" | sort -u | comm -23 <(cut -f1 "$WORK/modules") -)"
if [[ -n "$MISSING" ]]; then
  echo "third-party-notices: no license file found for these linked modules:" >&2
  sed 's/^/  /' <<<"$MISSING" >&2
  exit 1
fi

GOROOT_DIR="$(go env GOROOT)"
GOVERSION="$(go env GOVERSION)"
for f in LICENSE PATENTS; do
  [[ -f "$GOROOT_DIR/$f" ]] || die "Go toolchain $GOVERSION has no $f at its root"
done

rule() { printf '%s\n' "================================================================================"; }
emit_file() { # <label> <path>  — the file verbatim, newline-terminated
  printf -- '---- %s ----\n\n' "$1"
  cat "$2"
  [[ -n "$(tail -c1 "$2")" ]] && printf '\n'
  printf '\n'
}

NMOD="$(wc -l <"$WORK/modules" | tr -d ' ')"
NFILES="$(wc -l <"$WORK/files" | tr -d ' ')"
NNOTICE="$(cut -f4 "$WORK/files" | awk -F/ '{print toupper($NF)}' | grep -c '^NOTICE' || true)"

{
  echo "Third-party notices for twilightd"
  echo
  echo "The twilightd release binaries statically link the Go standard library and the"
  echo "third-party Go modules listed below. Each is distributed under its own license;"
  echo "the license, notice and patent-grant files each one ships are reproduced here"
  echo "verbatim. Twilight Core's own license is in LICENSE and NOTICE."
  echo
  echo "Generated by scripts/third-party-notices.sh from go.mod/go.sum; do not edit."
  echo "Release targets: $TARGETS (CGO_ENABLED=0)"
  echo "Go toolchain:    $GOVERSION"
  echo "Modules:         $NMOD third-party + the Go standard library"
  echo "Files:           $NFILES third-party ($NNOTICE NOTICE) + 2 Go"
  echo
  rule
  echo "Index"
  rule
  printf '%-58s %s\n' "std (Go standard library and runtime)" "$GOVERSION"
  while IFS=$'\t' read -r mod ver; do printf '%-58s %s\n' "$mod" "$ver"; done <"$WORK/modules"
  echo

  rule
  echo "Module:  std (Go standard library and runtime)"
  echo "Version: $GOVERSION"
  echo "Files:   LICENSE PATENTS"
  rule
  echo
  emit_file LICENSE "$GOROOT_DIR/LICENSE"
  emit_file PATENTS "$GOROOT_DIR/PATENTS"

  while IFS=$'\t' read -r mod ver; do
    rule
    echo "Module:  $mod"
    echo "Version: $ver"
    echo "Files:   $(awk -F'\t' -v m="$mod" '$1==m {print $4}' "$WORK/files" | tr '\n' ' ' | sed 's/ $//')"
    rule
    echo
    while IFS=$'\t' read -r _ _ abs r; do
      emit_file "$r" "$abs"
    done < <(awk -F'\t' -v m="$mod" '$1==m' "$WORK/files")
  done <"$WORK/modules"
} >"$WORK/out"

if [[ -n "$OUTPUT" ]]; then
  cp "$WORK/out" "$OUTPUT"
  echo "third-party-notices: $NMOD modules, $NFILES files ($NNOTICE NOTICE) -> $OUTPUT" >&2
else
  cat "$WORK/out"
fi
