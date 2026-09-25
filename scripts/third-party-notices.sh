#!/usr/bin/env bash
# Writes the third-party license and notice bundle for the twilightd release binaries.
#
# A release binary statically links every third-party Go module it imports, plus the
# Go standard library and runtime. Their licenses travel with the binary: BSD and MIT
# require the copyright and license text in binary redistributions, Apache-2.0 §4(d)
# requires a component's NOTICE file (CometBFT ships one) to be carried too, and
# MPL-2.0 §3.2(a) requires telling recipients of the executable where the source
# code is. Binaries alone do none of that, so each release also ships this bundle.
#
# What is covered is the set of modules ACTUALLY LINKED into ./cmd/twilightd — the
# union over the release targets, with the release's CGO_ENABLED=0 — not the whole
# go.mod graph, which also holds test-only and tool modules that never reach a
# binary. For each linked package the license files are collected from its own
# directory up to its module root, so a vendored subtree carrying its own license
# is included when (and only when) it is linked. A module-root licenses/ (or
# LICENSES/) directory is included whole: modules such as bytedance/sonic keep the
# licenses of code they bundle there rather than beside that code.
#
# The output is deterministic: same source tree, toolchain and targets give a
# byte-identical file. Nothing host-specific (module cache path, time, user) is
# written, modules are ordered by path and files by name in the C locale, and the
# content is read from the module cache, whose files are go.sum-verified.
#
# It resolves modules exactly as the release build does — `go list` under
# -mod=readonly, with no go.work and no user go env file — so it needs precisely
# the modules the build needs: it works offline (GOPROXY=off) whenever the build
# does, and a go.sum missing an entry the build needs fails here as it fails
# there. It never writes go.mod or go.sum, and checks that it did not.
#
# Exits non-zero, writing nothing, if any linked module ships no LICENSE/COPYING
# file: a release must not ship a component whose terms it cannot state, and a
# NOTICE or PATENTS file alone does not state them.
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
# The environment must not change what is listed, and must match build-release.sh:
# a go.work could redirect a module, and GOFLAGS — from the environment or from a
# user go env file, which an empty GOFLAGS does not override — could add build
# tags that link something else.
export GOENV=off GOWORK=off GOFLAGS=-mod=readonly CGO_ENABLED=0

die() { echo "third-party-notices: $*" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

cp go.mod "$WORK/go.mod.before"
cp go.sum "$WORK/go.sum.before"

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
cmp -s go.mod "$WORK/go.mod.before" || die "go.mod changed while listing; refusing"
cmp -s go.sum "$WORK/go.sum.before" || die "go.sum changed while listing; refusing"
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
# COPYING.txt, NOTICE and Go's PATENTS grant do. Matching is case-insensitive
# (nocasematch), so "license" and "Notice.txt" count.
shopt -s nocasematch
is_license_name() {
  [[ "$1" =~ ^(LICEN[CS]E|COPYING|NOTICE|PATENTS)([._-][A-Z0-9._-]*)?$ ]] || return 1
  [[ "$1" =~ \.(GO|S|C|H|PROTO|JSON|YAML|YML|SH|PY|TMPL|HTML|GOLDEN)$ ]] && return 1
  return 0
}
# Of those, the names that state license terms (NOTICE and PATENTS supplement them).
is_terms_name() { [[ "$1" =~ ^(LICEN[CS]E|COPYING) ]]; }

# module <TAB> version <TAB> absolute-file <TAB> file-relative-to-module <TAB> kind
# kind is "own" for files found on a linked package's path, "bundled" for files
# in a module-root licenses/ directory.
: >"$WORK/files"
while IFS=$'\t' read -r mod ver moddir rel; do
  dir="$moddir"; [[ "$rel" != "." ]] && dir="$moddir/$rel"
  for f in "$dir"/* "$dir"/.[!.]*; do
    [[ -f "$f" ]] || continue
    b="${f##*/}"
    is_license_name "$b" || continue
    r="$b"; [[ "$rel" != "." ]] && r="$rel/$b"
    printf '%s\t%s\t%s\t%s\town\n' "$mod" "$ver" "$f" "$r" >>"$WORK/files"
  done
  if [[ "$rel" == "." ]]; then
    for d in "$moddir"/*; do
      [[ -d "$d" && "${d##*/}" =~ ^LICEN[CS]ES$ ]] || continue
      while IFS= read -r f; do
        printf '%s\t%s\t%s\t%s\tbundled\n' "$mod" "$ver" "$f" "${f#"$moddir"/}" >>"$WORK/files"
      done < <(find "$d" -type f | sort)
    done
  fi
done <"$WORK/dirs"
sort -t $'\t' -k1,1 -k4,4 -u "$WORK/files" -o "$WORK/files"

cut -f1,2 "$WORK/pkgs" | sort -u >"$WORK/modules"
while IFS=$'\t' read -r mod _ _ r kind; do
  if [[ "$kind" == own ]] && is_terms_name "${r##*/}"; then printf '%s\n' "$mod"; fi
done <"$WORK/files" | sort -u >"$WORK/with-terms"
MISSING="$(cut -f1 "$WORK/modules" | comm -23 - "$WORK/with-terms")"
if [[ -n "$MISSING" ]]; then
  echo "third-party-notices: no LICENSE or COPYING file found for these linked modules:" >&2
  sed 's/^/  /' <<<"$MISSING" >&2
  exit 1
fi

GOROOT_DIR="$(go env GOROOT)"
GOVERSION="$(go env GOVERSION)"
for f in LICENSE PATENTS; do
  [[ -f "$GOROOT_DIR/$f" ]] || die "Go toolchain $GOVERSION has no $f at its root"
done

# The module proxy's case-encoding: each upper-case letter becomes "!" plus its
# lower-case form (golang.org/ref/mod#goproxy-protocol).
proxy_escape() { printf '%s' "$1" | sed 's/[A-Z]/!&/g' | tr 'A-Z' 'a-z'; }
source_of() { # <module> <version>
  case "$2" in
    "=> "*) echo "replaced in go.mod; see the replacement named under Version" ;;
    *) echo "https://proxy.golang.org/$(proxy_escape "$1")/@v/$(proxy_escape "$2").zip" ;;
  esac
}

# Modules whose own license files name the Mozilla Public License, listed in the
# header because MPL-2.0 §3.2(a) obliges saying where their source is.
while IFS=$'\t' read -r mod _ abs _ kind; do
  if [[ "$kind" == own ]] && grep -qi 'Mozilla Public License' "$abs"; then printf '%s\n' "$mod"; fi
done <"$WORK/files" | sort -u >"$WORK/mpl"

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
NMPL="$(wc -l <"$WORK/mpl" | tr -d ' ')"

{
  echo "Third-party notices for twilightd"
  echo
  echo "The twilightd release binaries statically link the Go standard library and the"
  echo "third-party Go modules listed below. Each is distributed under its own license;"
  echo "the license, notice and patent-grant files each one ships are reproduced here"
  echo "verbatim. Twilight Core's own license is in LICENSE and NOTICE."
  echo
  echo "Source code. The source code of every module listed below, at exactly the"
  echo "version listed, is available from its upstream repository and from the Go"
  echo "module proxy at https://proxy.golang.org/<module-path>/@v/<version>.zip (the"
  echo "URL is given under each module). The Go standard library's source is at"
  echo "https://go.dev/dl/ and https://go.googlesource.com/go (tag $GOVERSION). Twilight"
  echo "Core's own source is at https://github.com/twilight-project/twilight-core."
  echo "This is the notice of source availability that MPL-2.0 §3.2(a) requires for"
  echo "the modules distributed under the Mozilla Public License 2.0:"
  echo
  if [[ -s "$WORK/mpl" ]]; then
    while IFS= read -r m; do echo "  $m"; done <"$WORK/mpl"
  else
    echo "  (none)"
  fi
  echo
  echo "Generated by scripts/third-party-notices.sh from go.mod/go.sum; do not edit."
  echo "Release targets: $TARGETS (CGO_ENABLED=0)"
  echo "Go toolchain:    $GOVERSION"
  echo "Modules:         $NMOD third-party ($NMPL MPL-2.0) + the Go standard library"
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
  echo "Source:  https://go.googlesource.com/go/+/refs/tags/$GOVERSION"
  echo "Files:   LICENSE PATENTS"
  rule
  echo
  emit_file LICENSE "$GOROOT_DIR/LICENSE"
  emit_file PATENTS "$GOROOT_DIR/PATENTS"

  while IFS=$'\t' read -r mod ver; do
    rule
    echo "Module:  $mod"
    echo "Version: $ver"
    echo "Source:  $(source_of "$mod" "$ver")"
    echo "Files:   $(awk -F'\t' -v m="$mod" '$1==m {print $4}' "$WORK/files" | tr '\n' ' ' | sed 's/ $//')"
    rule
    echo
    while IFS=$'\t' read -r _ _ abs r _; do
      emit_file "$r" "$abs"
    done < <(awk -F'\t' -v m="$mod" '$1==m' "$WORK/files")
  done <"$WORK/modules"
} >"$WORK/out"

if [[ -n "$OUTPUT" ]]; then
  cp "$WORK/out" "$OUTPUT"
  echo "third-party-notices: $NMOD modules ($NMPL MPL-2.0), $NFILES files ($NNOTICE NOTICE) -> $OUTPUT" >&2
else
  cat "$WORK/out"
fi
