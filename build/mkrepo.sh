#!/bin/sh
# mkrepo.sh — build a package repository from a directory of .bb files
#
# Usage: ./build/mkrepo.sh <packages-dir> [output-dir]
#
# Creates BBINDEX.zst in output-dir, which bpm reads as the package index.

set -e

PKGS_DIR=${1:?usage: $0 <packages-dir> [output-dir]}
OUT_DIR=${2:-$PKGS_DIR}
BPM_BIN=${BPM_BIN:-bpm}

log() { printf '\033[1;32m==> %s\033[0m\n' "$*"; }

[ -d "$PKGS_DIR" ] || { echo "error: $PKGS_DIR is not a directory" >&2; exit 1; }
command -v zstd >/dev/null || { echo "error: zstd not found" >&2; exit 1; }

log "Indexing packages in $PKGS_DIR"

INDEX_TMP=$(mktemp)
trap 'rm -f "$INDEX_TMP"' EXIT

count=0
for bb in "$PKGS_DIR"/*.bb; do
    [ -f "$bb" ] || continue
    fname=$(basename "$bb")
    size=$(stat -c%s "$bb" 2>/dev/null || stat -f%z "$bb")
    sha256=$(sha256sum "$bb" | cut -d' ' -f1)

    # Extract manifest from the archive
    # Format: fields from .MANIFEST plus filename and sha256
    manifest=$(zstd -d < "$bb" | tar -xO .MANIFEST 2>/dev/null) || {
        echo "warn: could not read $fname" >&2
        continue
    }

    # Emit index record
    echo "$manifest" >> "$INDEX_TMP"
    echo "sha256: $sha256"  >> "$INDEX_TMP"
    echo "filename: $fname" >> "$INDEX_TMP"
    echo "size: $size"      >> "$INDEX_TMP"
    echo                    >> "$INDEX_TMP"

    count=$((count + 1))
done

log "Indexed $count packages"

# Compress the index
zstd -19 -q "$INDEX_TMP" -o "$OUT_DIR/BBINDEX.zst" --force
log "Wrote $OUT_DIR/BBINDEX.zst ($(du -sh "$OUT_DIR/BBINDEX.zst" | cut -f1))"
