#!/bin/bash
# tools/check-updates.sh — check upstream versions for BBUILDs
#
# Usage:
#   tools/check-updates.sh [<pkg>] [--pr]
#
#   (no args)           check all packages, print report
#   <pkg>               check only <pkg> (e.g. "musl")
#   --pr                for every update found: bump BBUILD, push branch, open draft PR
#
# When called from bump-package.sh for a single package, outputs:
#   → <old> → <new>    on the last line of the package block
#
# Requires: curl, jq, wget
# For --pr: gh CLI must be authenticated via GITHUB_TOKEN

set -euo pipefail

TOPDIR="$(cd "$(dirname "$0")/.." && pwd)"
PR_MODE=false
PKG_FILTER=""

for arg in "$@"; do
    case "$arg" in
        --pr)  PR_MODE=true ;;
        -*)    echo "Unknown flag: $arg" >&2; exit 1 ;;
        *)     PKG_FILTER="$arg" ;;
    esac
done

MAIN_BRANCH=$(git -C "$TOPDIR" symbolic-ref --short HEAD 2>/dev/null || echo master)

# ── Logging ───────────────────────────────────────────────────────────────────
info()    { printf '  %s\n' "$*"; }
success() { printf '\033[32m  ✓ %s\033[0m\n' "$*"; }
warn()    { printf '\033[33m  ! %s\033[0m\n' "$*"; }
found()   { printf '\033[36m  → %s\033[0m\n' "$*"; }

# ── Version fetch helpers ─────────────────────────────────────────────────────

# Latest GitHub release tag, stripping leading v/V.
# Falls back to tags API if no GitHub Releases are published (e.g. util-linux).
github_latest() {
    local repo="$1"
    local auth=()
    [[ -n "${GITHUB_TOKEN:-}" ]] && auth=(-H "Authorization: token $GITHUB_TOKEN")

    # Try releases first
    local tag
    tag=$(curl -sf "${auth[@]}" \
        "https://api.github.com/repos/$repo/releases/latest" \
        | jq -r '.tag_name // empty' 2>/dev/null || true)

    # Fall back to tags API (stable only: no -devel, -rc, -alpha, -beta)
    if [[ -z "$tag" ]]; then
        tag=$(curl -sf "${auth[@]}" \
            "https://api.github.com/repos/$repo/tags?per_page=20" \
            | jq -r '[.[].name | select(test("^v?[0-9]+\\.[0-9]+(\\.[0-9]+)?$"))] | .[0] // empty' \
            2>/dev/null || true)
    fi

    [[ -z "$tag" ]] && return 1
    # Strip leading v, openssl- prefix, underscores
    printf '%s\n' "$tag" | sed 's/^[vV]//;s/^openssl-//;s/^OpenSSL_//;s/_/./g'
}

# musl: read tags from the official cgit mirror
musl_latest() {
    curl -sf https://git.musl-libc.org/cgit/musl/refs/tags \
    | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' \
    | sort -V | tail -1 \
    | sed 's/^v//'
}

# busybox: parse the downloads listing
busybox_latest() {
    curl -sf https://busybox.net/downloads/ \
    | grep -oE 'busybox-[0-9]+\.[0-9]+\.[0-9]+\.tar\.bz2' \
    | sed 's/busybox-//;s/\.tar\.bz2//' \
    | sort -V | tail -1
}

# linux-headers: latest stable major.minor from kernel.org JSON
# (we track major.minor only, not patch, so 7.0.1 stays as 7.0)
kernel_headers_latest() {
    local fullver
    fullver=$(curl -sf https://www.kernel.org/releases.json \
        | jq -r '.releases[] | select(.moniker=="stable") | .version' \
        | sort -V | tail -1)
    # Return major.minor only (e.g. 7.0 from 7.0.3)
    printf '%s\n' "$fullver" | awk -F. '{print $1"."$2}'
}

# OpenSSH portable: parse the OpenBSD CDN listing
openssh_latest() {
    curl -sf https://cdn.openbsd.org/pub/OpenBSD/OpenSSH/portable/ \
    | grep -oE 'openssh-[0-9]+\.[0-9]+p[0-9]+\.tar\.gz' \
    | sed 's/openssh-//;s/\.tar\.gz//' \
    | sort -V | tail -1
}

# openssl: GitHub releases — skip pre-releases and pick highest stable tag
openssl_latest() {
    curl -sf \
        ${GITHUB_TOKEN:+-H "Authorization: token $GITHUB_TOKEN"} \
        "https://api.github.com/repos/openssl/openssl/releases" \
    | jq -r '[.[] | select(.prerelease==false) | .tag_name][0]' \
    | sed 's/^openssl-//'
}

# ── Package → version-check function mapping ─────────────────────────────────
declare -A UPSTREAM_FN=(
    [musl]="musl_latest"
    [busybox]="busybox_latest"
    [linux-headers]="kernel_headers_latest"
    [openssl]="openssl_latest"
    [util-linux]="github_latest util-linux/util-linux"
    [zlib]="github_latest madler/zlib"
    [openssh]="openssh_latest"
    # bpm is internal — no upstream check
    # runit rarely changes — no automated check
)

# ── Compute SHA-256 of a URL ──────────────────────────────────────────────────
sha256_of_url() {
    wget -q -O - "$1" | sha256sum | cut -d' ' -f1
}

# ── Process one BBUILD ────────────────────────────────────────────────────────
process_bbuild() {
    local bbuild="$1"
    local name version latest

    name=$(grep '^name=' "$bbuild" | head -1 | cut -d= -f2)
    version=$(grep '^version=' "$bbuild" | head -1 | cut -d= -f2)

    # Skip if a filter is set and this package doesn't match
    [[ -n "$PKG_FILTER" && "$name" != "$PKG_FILTER" ]] && return 0
    [[ -z "${UPSTREAM_FN[$name]+x}" ]] && return 0

    printf '\nChecking %-20s (current: %s)\n' "$name" "$version"

    local fn_args="${UPSTREAM_FN[$name]}"
    latest=$(eval "$fn_args" 2>/dev/null || true)

    if [[ -z "$latest" ]]; then
        warn "could not determine upstream version — skipping"
        return 0
    fi

    if [[ "$latest" == "$version" ]]; then
        success "up to date ($version)"
        return 0
    fi

    found "$version → $latest"
    (( found_updates++ )) || true

    $PR_MODE || return 0

    # ── Patch the BBUILD ────────────────────────────────────────────────────────
    local branch="auto-update/${name}-${latest}"

    # Extract source URL template and substitute new version
    local src_template new_url
    src_template=$(grep '^source=(' "$bbuild" | head -1 \
        | sed "s/source=(//;s/)//;s/[\"']//g" | xargs)
    new_url=$(printf '%s' "$src_template" \
        | sed "s/\$version/$latest/g;s/\${version}/$latest/g" \
        | sed "s/\$name/$name/g;s/\${name}/$name/g")

    # Compute new checksum
    info "Fetching $new_url for checksum..."
    local new_sha
    new_sha=$(sha256_of_url "$new_url" 2>/dev/null || echo "")
    if [[ -z "$new_sha" ]]; then
        warn "could not fetch source — skipping PR"
        return 0
    fi
    info "sha256: $new_sha"

    # Apply patch to a temp copy
    local tmp
    tmp=$(mktemp)
    cp "$bbuild" "$tmp"

    # Bump version=
    sed -i "s/^version=${version}$/version=${latest}/" "$tmp"
    # Bump release= back to 1
    sed -i 's/^release=[0-9]*/release=1/' "$tmp"
    # Replace old sha256 checksum
    local old_sha
    old_sha=$(grep -oE '[0-9a-f]{64}' "$bbuild" | head -1 || true)
    if [[ -n "$old_sha" ]] && [[ "$new_sha" != "$old_sha" ]]; then
        sed -i "s/$old_sha/$new_sha/" "$tmp"
    fi

    # Skip if BBUILD is unchanged
    if diff -q "$bbuild" "$tmp" > /dev/null 2>&1; then
        warn "no effective change after patching — skipping PR"
        rm -f "$tmp"
        return 0
    fi

    # ── Commit + push + PR ────────────────────────────────────────────────────
    git -C "$TOPDIR" checkout -b "$branch" "$MAIN_BRANCH" 2>/dev/null || \
        git -C "$TOPDIR" checkout "$branch"

    cp "$tmp" "$bbuild"
    rm -f "$tmp"

    git -C "$TOPDIR" add "$bbuild"
    git -C "$TOPDIR" commit -m "chore(pkgs): update $name $version → $latest

Auto-generated by tools/check-updates.sh"

    git -C "$TOPDIR" push origin "$branch" 2>/dev/null || \
        git -C "$TOPDIR" push --set-upstream origin "$branch"

    gh pr create \
        --repo "$(git -C "$TOPDIR" remote get-url origin | sed 's|.*github.com[:/]||;s|\.git$||')" \
        --title "chore(pkgs): update $name $version → $latest" \
        --body "## Automated version bump

| Field | Value |
|-------|-------|
| Package | \`$name\` |
| Before | \`$version\` |
| After  | \`$latest\` |
| Checksum updated | yes |

---
_Auto-generated by \`tools/check-updates.sh\`_" \
        --base "$MAIN_BRANCH" \
        --draft \
        2>/dev/null && info "PR created" || warn "PR creation failed (may already exist)"

    git -C "$TOPDIR" checkout "$MAIN_BRANCH"
}

# ── Main ─────────────────────────────────────────────────────────────────────
found_updates=0
total=0

printf 'Checking package versions...\n'

for bbuild in $(find "$TOPDIR/pkgs" -name BBUILD | sort); do
    process_bbuild "$bbuild"
    (( total++ )) || true
done

printf '\n'
if [[ "$found_updates" -eq 0 ]]; then
    printf 'All %d packages are up to date.\n' "$total"
else
    printf '%d update(s) found across %d packages.\n' "$found_updates" "$total"
    $PR_MODE && printf 'PRs created for all updates.\n' || \
        printf 'Re-run with --pr to create GitHub PRs.\n'
fi
