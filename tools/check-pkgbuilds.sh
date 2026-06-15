#!/usr/bin/env bash
# check-pkgbuilds.sh — static validation of packages/*/PKGBUILD without building.
#
# Catches recipe rot (missing fields, malformed checksums, source/sum length
# mismatch, SKIP'd checksums) in CI and locally, without running makepkg or
# pulling an Arch container. Exit non-zero if any recipe is invalid.
#
# Usage: tools/check-pkgbuilds.sh [packages-dir]   (default: ./packages)

set -u
DIR="${1:-packages}"
[ -d "$DIR" ] || { echo "check-pkgbuilds: no such dir: $DIR" >&2; exit 2; }

rc=0
fail() { printf '  ✘ %s\n' "$1"; rc=1; }

for pb in "$DIR"/*/PKGBUILD; do
    [ -f "$pb" ] || continue
    name=$(basename "$(dirname "$pb")")
    printf '== %s\n' "$name"

    # Syntax check first — a parse error would derail sourcing.
    if ! bash -n "$pb" 2>/dev/null; then
        fail "bash syntax error"; continue
    fi

    # Source in a clean subshell to read the declarative fields. PKGBUILDs only
    # assign variables / define functions at top level, so sourcing is safe.
    # shellcheck disable=SC1090
    eval "$(
        bash -c '
            set -u
            source "'"$pb"'" >/dev/null 2>&1
            declare -p pkgname pkgver pkgrel arch source sha256sums 2>/dev/null
        '
    )" 2>/dev/null

    [ -n "${pkgname:-}" ] || fail "pkgname unset"
    [ "${pkgname:-}" = "$name" ] || fail "pkgname '${pkgname:-}' != directory '$name'"
    [ -n "${pkgver:-}" ]  || fail "pkgver unset"
    [ -n "${pkgrel:-}" ]  || fail "pkgrel unset"
    [ -n "${arch:-}" ]    || fail "arch unset"

    # source[] and sha256sums[] must exist and line up 1:1.
    local_src=( "${source[@]:-}" )
    local_sum=( "${sha256sums[@]:-}" )
    if [ "${#local_src[@]}" -eq 0 ] || [ -z "${local_src[0]}" ]; then
        fail "source[] empty"
    fi
    if [ "${#local_sum[@]}" -ne "${#local_src[@]}" ]; then
        fail "sha256sums (${#local_sum[@]}) != source (${#local_src[@]}) length"
    fi
    # Every checksum must be 64 hex chars (a real sha256) — flag SKIP / stubs.
    for s in "${local_sum[@]:-}"; do
        case "$s" in
            SKIP)  fail "checksum is SKIP (unverified source)";;
            *) [[ "$s" =~ ^[0-9a-f]{64}$ ]] || fail "bad sha256sum: '$s'";;
        esac
    done

    unset pkgname pkgver pkgrel arch source sha256sums
done

if [ "$rc" -eq 0 ]; then
    echo "check-pkgbuilds: all recipes valid"
else
    echo "check-pkgbuilds: FAILURES above" >&2
fi
exit "$rc"
