#!/bin/sh
# check-base-closure.sh — verify every ELF in a base rootfs can resolve all of
# its DT_NEEDED shared libraries from within that same rootfs.
#
# The base image is assembled from a FLAT package list with no dependency
# resolution (see GNUmakefile SYSTEMD_BASE_PKGS), so it is easy to ship a tool
# without the runtime library it links against — e.g. grep without libpcre2, or
# gawk without libmpfr. This scans the assembled rootfs and reports any such gap,
# so the mistake is caught at build time instead of on the running system.
#
# Usage: check-base-closure.sh <rootdir>
# Exit:  0 = closure complete, 1 = missing libraries, 2 = usage/tooling error.
set -eu
ROOT=${1:?usage: check-base-closure.sh <rootdir>}
[ -d "$ROOT" ] || { echo "check-base-closure: no such rootdir: $ROOT" >&2; exit 2; }
command -v readelf >/dev/null 2>&1 || { echo "check-base-closure: readelf not found" >&2; exit 2; }

provided=$(mktemp); fail=$(mktemp)
trap 'rm -f "$provided" "$fail"' EXIT

# Every shared library the rootfs ships (by basename), incl. the linker itself.
for d in usr/lib usr/lib64 lib lib64 usr/lib/security; do
    [ -d "$ROOT/$d" ] && find "$ROOT/$d" -maxdepth 3 \( -name '*.so' -o -name '*.so.*' \) -printf '%f\n' 2>/dev/null
done | sort -u > "$provided"
# ld.so is resolved by the kernel, not via DT_NEEDED, but list it anyway.
printf 'ld-linux-x86-64.so.2\nld-linux.so.2\n' >> "$provided"

# Scan every executable + shared object for NEEDED libs missing from the rootfs.
find "$ROOT" -type f \( -path '*/bin/*' -o -path '*/sbin/*' -o -name '*.so' -o -name '*.so.*' \) 2>/dev/null \
| while IFS= read -r f; do
    needed=$(readelf -d "$f" 2>/dev/null | sed -n 's/.*(NEEDED).*\[\(.*\)\].*/\1/p') || continue
    [ -n "$needed" ] || continue
    for lib in $needed; do
        grep -qxF "$lib" "$provided" || printf '%s\tneeds\t%s\n' "${f#"$ROOT"}" "$lib" >> "$fail"
    done
done

if [ -s "$fail" ]; then
    echo "check-base-closure: FAIL — base ELFs need libraries the base does not ship:" >&2
    sort -u "$fail" | sed 's/^/  /' >&2
    echo "  → add the owning package(s) to SYSTEMD_BASE_PKGS in GNUmakefile." >&2
    exit 1
fi
echo "check-base-closure: OK — every DT_NEEDED library is provided by the base rootfs"
