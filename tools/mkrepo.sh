#!/bin/sh
# mkrepo.sh — build a bpm repository index from a directory of .pkg.tar.zst files.
#
# Usage: tools/mkrepo.sh <repo-dir>
#   Reads every *.pkg.tar.zst in <repo-dir>, extracts its .PKGINFO, and writes
#   <repo-dir>/bpm.index. Serve <repo-dir> over HTTP and point a client's
#   /etc/bpm/repos.conf at it; `bpm update` fetches bpm.index from there.
#
# Index line:  name|version|filename|sha256|dep1,dep2,...
#
# Signing: if a signing key is available (BPM_SIGN_KEY env, default
#   ~/.config/bpm/repo-signing-key.pem), the index is signed with ECDSA
#   P-256/SHA-256 and the detached DER signature written to bpm.index.sig.
#   bpm verifies that against the public key baked into src/bpm-rs/src/repokey.rs.
#   To rotate keys: regenerate the keypair (see KEYGEN below) and re-emit the
#   header, then rebuild bpm.
#
# KEYGEN:
#   openssl ecparam -name prime256v1 -genkey -noout -out repo-signing-key.pem
#   # re-bake the public point into src/bpm-rs/src/repokey.rs (tools/mkrepokey.sh)

set -eu
REPO="${1:-.}"
[ -d "$REPO" ] || { echo "mkrepo: no such dir: $REPO" >&2; exit 1; }
for t in zstd tar sha256sum; do
    command -v "$t" >/dev/null 2>&1 || { echo "mkrepo: need $t" >&2; exit 1; }
done
SIGN_KEY="${BPM_SIGN_KEY:-$HOME/.config/bpm/repo-signing-key.pem}"

field() { awk -v k="$1" -F ' = ' '$1==k{print $2}'; }
pkginfo() { zstd -dcq "$1" | tar -xO -f - .PKGINFO 2>/dev/null \
            || zstd -dcq "$1" | tar -xO -f - ./.PKGINFO 2>/dev/null; }

out="$REPO/bpm.index"
: > "$out.tmp"
n=0
for pkg in "$REPO"/*.pkg.tar.zst; do
    [ -f "$pkg" ] || continue
    info=$(pkginfo "$pkg") || { echo "mkrepo: skip (no .PKGINFO): $pkg" >&2; continue; }
    name=$(printf '%s\n' "$info" | field pkgname)
    ver=$(printf '%s\n' "$info" | field pkgver)
    [ -n "$name" ] || { echo "mkrepo: skip (no pkgname): $pkg" >&2; continue; }
    deps=$(printf '%s\n' "$info" | field depend | paste -sd, -)
    sha=$(sha256sum "$pkg" | cut -d' ' -f1)
    printf '%s|%s|%s|%s|%s\n' "$name" "$ver" "$(basename "$pkg")" "$sha" "$deps" >> "$out.tmp"
    n=$((n + 1))
done
sort -o "$out.tmp" "$out.tmp"
mv "$out.tmp" "$out"
echo "mkrepo: wrote $out ($n packages)"

# Sign the index (detached ECDSA-P256/SHA-256, DER) if a key is available.
if [ -f "$SIGN_KEY" ]; then
    if command -v openssl >/dev/null 2>&1; then
        openssl dgst -sha256 -sign "$SIGN_KEY" -out "$out.sig" "$out"
        echo "mkrepo: signed $out.sig with $SIGN_KEY"
    else
        echo "mkrepo: WARNING: openssl missing, cannot sign index" >&2
    fi
else
    echo "mkrepo: no signing key at $SIGN_KEY — index left UNSIGNED" >&2
    echo "mkrepo: clients with BPM_ALLOW_UNSIGNED unset will reject it" >&2
    rm -f "$out.sig"
fi
