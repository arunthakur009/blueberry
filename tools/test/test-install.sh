#!/bin/bash
# test-install.sh — automated end-to-end install smoke-test (QEMU, BIOS).
#
# Boots the kernel+initramfs in unattended-install mode (`bbinstall`) with the
# given installer ISO attached as the payload medium and a blank virtio disk as
# the target; asserts BLUEBERRY_INSTALL=OK on serial, then boots the installed
# disk and asserts it reaches multi-user with a login prompt.
#
# Usage: tools/test/test-install.sh <installer.iso>   (desktop or server payload)
set -u

ISO=${1:?usage: $0 <installer.iso>}
BOOTDIR=${BOOTDIR:-$(dirname "$0")/../../../blueberry-build/boot}
WORK=${WORK:-$(dirname "$ISO")/../../blueberry-build/installtest}
[ -f "$ISO" ] || { echo "FAIL: ISO not found: $ISO"; exit 1; }
VMLINUZ="$BOOTDIR/vmlinuz"; INITRD="$BOOTDIR/initramfs.cpio.zst"
[ -f "$VMLINUZ" ] || { echo "FAIL: $VMLINUZ missing (run 'make install')"; exit 1; }

rm -rf "$WORK"; mkdir -p "$WORK"
DISK="$WORK/target.qcow2"; ILOG="$WORK/install-serial.log"; BLOG="$WORK/boot-serial.log"
qemu-img create -f qcow2 "$DISK" 12G >/dev/null
ACCEL="-enable-kvm -cpu host"; [ -w /dev/kvm ] || ACCEL="-cpu max"

echo "[install-test] unattended install ($ISO)…"
timeout 900 qemu-system-x86_64 $ACCEL -m 3072 -smp 2 \
    -kernel "$VMLINUZ" -initrd "$INITRD" \
    -append "bbinstall blueberry.target=/dev/vda blueberry.bootloader=bios blueberry.rootpw=blueberry blueberry.hostname=bbtest blueberry.user=blueberry blueberry.userpw=blueberry console=ttyS0,115200" \
    -cdrom "$ISO" -drive "file=$DISK,if=virtio,format=qcow2" \
    -nic user,model=virtio-net-pci \
    -serial "file:$ILOG" -display none -no-reboot

if ! grep -qaE "BLUEBERRY_INSTALL=OK|BLUEBERRY_INSTALL_EXIT=0" "$ILOG"; then
    echo "[install-test] FAIL — install did not complete. Serial tail:"
    tail -25 "$ILOG" | sed 's/\x1b\[[0-9;]*m//g'
    exit 1
fi
echo "[install-test] install OK — booting the INSTALLED disk…"

timeout 180 qemu-system-x86_64 $ACCEL -m 3072 -smp 2 \
    -drive "file=$DISK,if=virtio,format=qcow2" -boot c \
    -serial "file:$BLOG" -display none -no-reboot &
QP=$!
for i in $(seq 1 55); do
    sleep 3
    if grep -qaE "login:|Reached target Multi-User System" "$BLOG" 2>/dev/null; then
        RESULT=multiuser; break
    fi
done
# Let trailing unit-start lines (sshd, networkd) flush to the serial log before
# we tear the VM down — multi-user.target prints as its wants are still settling.
[ "${RESULT:-none}" = multiuser ] && sleep 6
kill -9 $QP 2>/dev/null; wait 2>/dev/null

if [ "${RESULT:-none}" != multiuser ]; then
    echo "[install-test] FAIL — installed disk did not reach a ready target. Serial tail:"
    tail -25 "$BLOG" | sed 's/\x1b\[[0-9;]*m//g'
    exit 1
fi
echo "[install-test] reached multi-user — checking service health…"

# strip ANSI once for the health greps
CLEAN="$WORK/boot-clean.log"; sed 's/\x1b\[[0-9;]*m//g' "$BLOG" > "$CLEAN"

# Hard fail on any unit that failed to start (systemd prints "Failed to start …").
if grep -qaE "Failed to start|\[FAILED\]" "$CLEAN"; then
    echo "[install-test] FAIL — a service failed to start on the installed system:"
    grep -aE "Failed to start|\[FAILED\]" "$CLEAN" | head -8
    exit 1
fi

# Positively confirm the two services the server image enables actually came up.
miss=
grep -qaE "Started OpenSSH server daemon|Reached target .*Login|sshd" "$CLEAN" || miss="$miss sshd"
grep -qaE "Started Network Configuration|Reached target Network|systemd-networkd" "$CLEAN" || miss="$miss networkd"
if [ -n "$miss" ]; then
    echo "[install-test] FAIL — expected service(s) never started:$miss"
    echo "  (serial log had no start marker; tail below)"
    tail -25 "$CLEAN"
    exit 1
fi

echo "[install-test] PASS — installed disk reached multi-user; sshd + networkd up, no failed units"
