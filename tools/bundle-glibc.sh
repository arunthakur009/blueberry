#!/bin/bash
# bundle-glibc.sh — stage the host's glibc runtime into a destination root so
# the image can run dynamically-linked glibc binaries: the system's own
# (busybox/runit/dropbear) AND external prebuilt software (the whole point of
# using glibc instead of musl).
#
# Usage: bundle-glibc.sh <destroot> [binary ...]
#   Copies the ELF interpreter, the ldd deps of each <binary>, a compat set of
#   common shared libs, and the dlopen-only NSS modules; then builds ld.so.cache.
#
# Runs on the build host (which is glibc). Idempotent.

set -eu

DEST=${1:?usage: $0 <destroot> [binary ...]}
shift || true

LIBDIR="$DEST/usr/lib"
mkdir -p "$DEST/lib64" "$LIBDIR" "$DEST/etc"
# Merged-usr layout: /lib -> usr/lib. /lib64 stays a real dir for the linker.
[ -e "$DEST/lib" ] || ln -sf usr/lib "$DEST/lib"

# The ELF interpreter path is hard-coded into every dynamic binary.
cp -Lf /lib64/ld-linux-x86-64.so.2 "$DEST/lib64/ld-linux-x86-64.so.2"

copy_lib() {
    local src="$1" base
    [ -n "$src" ] && [ -e "$src" ] || return 0
    base=$(basename "$src")
    [ -e "$LIBDIR/$base" ] && return 0
    cp -Lf "$src" "$LIBDIR/$base"
}

resolve_soname() {  # find a library on the host by its SONAME
    ldconfig -p | awk -v s="$1" '$1==s {print $NF; exit}'
}

# 1. Direct (ldd) dependencies of the binaries we ship.
for bin in "$@"; do
    [ -e "$bin" ] || continue
    while read -r dep; do
        copy_lib "$dep"
    done < <(ldd "$bin" 2>/dev/null | awk '/=> \//{print $3}')
done

# 2. Compat set: common libs external glibc software expects, plus the
#    runtime-dlopen'd NSS modules (which ldd never reports). Without nss_files /
#    nss_dns, getpwnam() and DNS resolution silently fail.
for soname in \
    libc.so.6 libm.so.6 libdl.so.2 librt.so.1 libpthread.so.0 \
    libresolv.so.2 libcrypt.so.2 libz.so.1 \
    libstdc++.so.6 libgcc_s.so.1 \
    libnss_files.so.2 libnss_dns.so.2 ; do
    copy_lib "$(resolve_soname "$soname")"
done

# 3. ld.so.cache so the interpreter finds everything in /usr/lib at runtime.
ldconfig -r "$DEST" 2>/dev/null || true

echo "[bundle-glibc] staged $(ls "$LIBDIR" | wc -l) libs + linker into $DEST"
