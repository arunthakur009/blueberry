#!/bin/sh
# bootstrap.sh — build a Blueberry Linux root filesystem from scratch
#
# Usage: ./build/bootstrap.sh <destdir> [arch]
#   destdir   target root filesystem directory
#   arch      target architecture (default: x86_64)
#
# Prerequisites (on the build host):
#   musl-gcc, make, wget/curl, go >=1.22, tar, zstd, xz

set -e
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(dirname "$SCRIPT_DIR")

DESTDIR=${1:?usage: $0 <destdir> [arch]}
ARCH=${2:-x86_64}
JOBS=$(nproc 2>/dev/null || echo 4)

BPM="$REPO_ROOT/tools/bpm"
PKGS="$REPO_ROOT/pkgs/core"
ROOTFS="$REPO_ROOT/rootfs"
BUILD_TMP=$(mktemp -d /tmp/blueberry-bootstrap.XXXXXX)

trap 'rm -rf "$BUILD_TMP"' EXIT

log() { printf '\033[1;32m==> %s\033[0m\n' "$*"; }
die() { printf '\033[1;31mFATAL: %s\033[0m\n' "$*" >&2; exit 1; }

# ── 0. Sanity checks ──────────────────────────────────────────────────────────
command -v musl-gcc  >/dev/null || die "musl-gcc not found"
command -v go        >/dev/null || die "go not found"
command -v wget      >/dev/null || command -v curl >/dev/null || die "wget or curl required"

# ── 1. Build bpm binary ───────────────────────────────────────────────────────
log "Building bpm package manager"
(
    cd "$BPM"
    CGO_ENABLED=0 GOARCH="$ARCH" go build \
        -ldflags="-s -w" \
        -trimpath \
        -o "$BUILD_TMP/bpm" .
)

# ── 2. Create base directory structure ───────────────────────────────────────
log "Creating base directory structure in $DESTDIR"
mkdir -p "$DESTDIR"/{bin,sbin,lib,lib64,usr/{bin,sbin,lib,lib64,include,share},
          etc/{bpm/repos.d,runit,sv,sysctl.d,profile.d,ssh,ssl},
          dev,proc,sys,run,tmp,var/{lib/bpm/{db/installed,cache/{packages,indices}},
          log,spool/cron,empty},root,home,mnt,boot,srv}

chmod 1777 "$DESTDIR/tmp"
chmod 700  "$DESTDIR/root"
chmod 711  "$DESTDIR/var/empty"

# ── 3. Install bpm ───────────────────────────────────────────────────────────
log "Installing bpm"
install -m755 "$BUILD_TMP/bpm" "$DESTDIR/usr/bin/bpm"

# ── 4. Copy rootfs overlay ───────────────────────────────────────────────────
log "Installing rootfs overlay"
cp -a "$ROOTFS"/. "$DESTDIR/"

# ── 5. Build and install core packages ───────────────────────────────────────
log "Building core packages (this takes a while)"

build_pkg() {
    local bbuild="$1"
    local name
    name=$(grep '^name=' "$bbuild" | head -1 | cut -d= -f2 | tr -d '"')
    log "  Building $name"
    "$BUILD_TMP/bpm" build \
        --output "$BUILD_TMP/pkgs" \
        --workdir "$BUILD_TMP/work-$name" \
        --arch "$ARCH" \
        --jobs "$JOBS" \
        "$bbuild" || die "Failed to build $name"
}

mkdir -p "$BUILD_TMP/pkgs"

# Build order: dependencies before dependents
for pkg in musl linux-headers zlib openssl busybox util-linux runit bpm openssh; do
    bbuild="$PKGS/$pkg/BBUILD"
    [ -f "$bbuild" ] && build_pkg "$bbuild"
done

# ── 6. Install built packages into destdir ───────────────────────────────────
log "Installing packages into $DESTDIR"
for bb in "$BUILD_TMP/pkgs"/*.bb; do
    [ -f "$bb" ] || continue
    "$BUILD_TMP/bpm" install \
        --root "$DESTDIR" \
        --file \
        --yes \
        "$bb"
done

# ── 7. Configure runit stage scripts ─────────────────────────────────────────
log "Configuring init (runit)"
install -Dm755 "$REPO_ROOT/init/runit/1" "$DESTDIR/etc/runit/1"
install -Dm755 "$REPO_ROOT/init/runit/2" "$DESTDIR/etc/runit/2"
install -Dm755 "$REPO_ROOT/init/runit/3" "$DESTDIR/etc/runit/3"

# Install default service definitions
for svc in "$REPO_ROOT/init/runit/sv"/*/; do
    svcname=$(basename "$svc")
    install -dm755 "$DESTDIR/etc/sv/$svcname"
    install -Dm755 "$svc/run" "$DESTDIR/etc/sv/$svcname/run"
    [ -f "$svc/finish" ] && install -Dm755 "$svc/finish" "$DESTDIR/etc/sv/$svcname/finish"
done

# Enable getty on tty1 by default
mkdir -p "$DESTDIR/etc/sv/enabled"
ln -sf /etc/sv/getty-tty1 "$DESTDIR/etc/sv/enabled/getty-tty1"
ln -sf /etc/sv/syslogd    "$DESTDIR/etc/sv/enabled/syslogd"

# ── 8. Set root password (locked — must be set on first boot) ─────────────────
if command -v chroot >/dev/null 2>&1; then
    log "Setting root password (locked — use 'passwd root' after first boot)"
    chroot "$DESTDIR" /usr/sbin/usermod -L root 2>/dev/null || true
fi

# ── 9. Final touches ─────────────────────────────────────────────────────────
log "Finalizing"
ln -sf /usr/bin/bpm "$DESTDIR/usr/sbin/bpm"

# Ensure /init symlink for direct kernel booting
ln -sf /sbin/runit-init "$DESTDIR/init"

cat <<EOF

Blueberry Linux rootfs built at: $DESTDIR

Next steps:
  1. Customize /etc/hostname, /etc/fstab, network config
  2. Set a root password: arch-chroot $DESTDIR passwd root
  3. Build a bootable image:  ./build/mkiso.sh $DESTDIR
  4. Or install to disk:      ./build/mkdisk.sh $DESTDIR /dev/sdX

EOF
