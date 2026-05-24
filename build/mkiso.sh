#!/bin/sh
# mkiso.sh — create a bootable ISO from a Blueberry rootfs
#
# Usage: ./build/mkiso.sh <rootfsdir> [output.iso]
#
# Requires: grub2, xorriso (or genisoimage), squashfs-tools

set -e

ROOTFS=${1:?usage: $0 <rootfsdir> [output.iso]}
OUTPUT=${2:-blueberry-$(date +%Y%m%d).iso}
BUILD_TMP=$(mktemp -d /tmp/blueberry-iso.XXXXXX)
trap 'rm -rf "$BUILD_TMP"' EXIT

log() { printf '\033[1;32m==> %s\033[0m\n' "$*"; }
die() { printf '\033[1;31mFATAL: %s\033[0m\n' "$*" >&2; exit 1; }

[ -d "$ROOTFS" ] || die "rootfs directory not found: $ROOTFS"
command -v mksquashfs >/dev/null || die "mksquashfs not found (install squashfs-tools)"
command -v xorriso    >/dev/null || die "xorriso not found"

ISO_ROOT="$BUILD_TMP/iso"
mkdir -p "$ISO_ROOT/boot/grub"

# ── 1. Build squashfs of the rootfs ──────────────────────────────────────────
log "Creating squashfs image"
mksquashfs "$ROOTFS" "$ISO_ROOT/boot/rootfs.squashfs" \
    -comp zstd -Xcompression-level 19 \
    -noappend -no-progress \
    -e boot

# ── 2. Copy kernel and initramfs ─────────────────────────────────────────────
log "Copying kernel"
VMLINUZ=$(find "$ROOTFS/boot" -name 'vmlinuz*' | head -1)
INITRD=$(find "$ROOTFS/boot"  -name 'initramfs*' -o -name 'initrd*' | head -1)

[ -n "$VMLINUZ" ] || die "No kernel found in $ROOTFS/boot"
cp "$VMLINUZ" "$ISO_ROOT/boot/vmlinuz"
[ -n "$INITRD" ] && cp "$INITRD" "$ISO_ROOT/boot/initramfs.img"

# ── 3. Write GRUB config ─────────────────────────────────────────────────────
cat > "$ISO_ROOT/boot/grub/grub.cfg" <<'EOF'
set timeout=5
set default=0

menuentry "Blueberry Linux" {
    linux /boot/vmlinuz root=live:CDLABEL=BLUEBERRY rw quiet
    initrd /boot/initramfs.img
}

menuentry "Blueberry Linux (verbose)" {
    linux /boot/vmlinuz root=live:CDLABEL=BLUEBERRY rw
    initrd /boot/initramfs.img
}
EOF

# ── 4. Create ISO ─────────────────────────────────────────────────────────────
log "Creating ISO: $OUTPUT"
xorriso -as mkisofs \
    -iso-level 3 \
    -full-iso9660-filenames \
    -volid "BLUEBERRY" \
    -eltorito-boot boot/grub/i386-pc/eltorito.img \
    -no-emul-boot \
    -boot-load-size 4 \
    -boot-info-table \
    --eltorito-catalog boot/grub/boot.cat \
    --grub2-boot-info \
    --grub2-mbr /usr/lib/grub/i386-pc/boot_hybrid.img \
    -eltorito-alt-boot \
    -e boot/grub/efi.img \
    -no-emul-boot \
    -append_partition 2 0xEF "$ISO_ROOT/boot/grub/efi.img" \
    -output "$OUTPUT" \
    "$ISO_ROOT"

log "ISO written to $OUTPUT ($(du -sh "$OUTPUT" | cut -f1))"
