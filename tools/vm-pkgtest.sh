#!/bin/bash
# vm-pkgtest.sh — boot the live CLI headless, install packages from the repo,
# run each binary, and assert none die on a missing shared library.
#
# Drives the serial console, but first `exec sh` to leave bash's readline (its
# line redraw mangles fast piped input over serial); busybox sh echoes via the
# tty driver, so commands and output stay clean.
#
# Usage: tools/vm-pkgtest.sh [pkg:binary[:args] ...]
# Env: BOOTDIR, MEM (768M), TIMEOUT (300s), BOOT_DELAY (24s).
set -u

TOPDIR="$(cd "$(dirname "$0")/.." && pwd)"
BOOTDIR="${BOOTDIR:-$(cd "$TOPDIR/.." && pwd)/blueberry-build/boot}"
MEM="${MEM:-768M}"; TIMEOUT="${TIMEOUT:-300}"; BOOT_DELAY="${BOOT_DELAY:-24}"
KERNEL="$BOOTDIR/vmlinuz"; INITRD="$BOOTDIR/initramfs.cpio.zst"
[ -f "$KERNEL" ] && [ -f "$INITRD" ] || { echo "missing boot artifacts in $BOOTDIR" >&2; exit 2; }

PAIRS=("$@")
if [ ${#PAIRS[@]} -eq 0 ]; then
  PAIRS=(nano:nano:--version file:file:--version less:less:--version
         tcpdump:tcpdump:--version python:python3:--version git:git:--version
         redis:redis-server:--version screen:screen:--version sqlite:sqlite3:--version
         strace:strace:--version pciutils:lspci:--version wget:wget:--version
         iptables:iptables:--version mdadm:mdadm:--version chrony:chronyd:--version)
fi
pkgs=""; for p in "${PAIRS[@]}"; do pkgs="$pkgs ${p%%:*}"; done

script="$(mktemp)"; log="$(mktemp)"
{
  echo 'exec sh'                         # leave bash/readline → clean serial I/O
  echo 'sleep 6'                         # DHCP settle
  echo "bpm update && bpm install$pkgs"
  echo 'echo BBMARK_INSTALLED'
  for p in "${PAIRS[@]}"; do
    bin="${p#*:}"; cmd="${bin%%:*}"; args="${bin#*:}"; [ "$args" = "$bin" ] && args=""
    # one simple line per binary; capture first output line and tag with status
    echo "$cmd $args >/tmp/o 2>/tmp/e; echo \"RT $cmd rc=\$? | \$(cat /tmp/o /tmp/e 2>/dev/null | grep . | head -1)\""
  done
  echo 'echo BBMARK_DONE'
  echo 'poweroff -f'
} > "$script"

echo "[vmtest] packages:$pkgs"
MACHINE_ARGS=(); [ -w /dev/kvm ] && MACHINE_ARGS+=(-enable-kvm -cpu host)
( sleep "$BOOT_DELAY"; cat "$script"; sleep "$TIMEOUT" ) | \
  timeout "$((TIMEOUT+50))" qemu-system-x86_64 "${MACHINE_ARGS[@]}" \
    -nic user,model=e1000 -kernel "$KERNEL" -initrd "$INITRD" \
    -append "console=ttyS0 bonding.max_bonds=0 dummy.numdummies=0" \
    -m "$MEM" -no-reboot -nographic > "$log" 2>&1

# Strip ANSI/CR and pull the result lines.
clean=$(sed 's/\x1b\[[0-9?]*[a-zA-Z]//g; s/\r//g' "$log")
echo "──────────────────────────────────────────────"
printf '%s\n' "$clean" | grep -E '^RT |BBMARK_' | grep -v 'echo'
echo "──────────────────────────────────────────────"
# The tool detects MISSING LIBRARIES (the bug class), not --version exit codes:
# a binary that fails to load prints "cannot open shared object" and never runs.
# Some tools exit non-zero for --version (lspci has none; less/wget quirks), so
# nonzero rc is only a note as long as the loader didn't fail.
miss=$(printf '%s\n' "$clean" | grep -c 'cannot open shared object')
notfound=$(printf '%s\n' "$clean" | grep -E '^RT ' | grep -c 'rc=127')
ran=$(printf '%s\n' "$clean" | grep -cE '^RT ')
nonzero=$(printf '%s\n' "$clean" | grep -E '^RT ' | grep -vE 'rc=0 ' | grep -c .)
if printf '%s\n' "$clean" | grep -q BBMARK_DONE && [ "$miss" -eq 0 ] && [ "$notfound" -eq 0 ] && [ "$ran" -ge "${#PAIRS[@]}" ]; then
  echo "[vmtest] RESULT: PASS — $ran binaries loaded, 0 missing libraries ($nonzero had non-zero --version rc, not a load failure)"
  rm -f "$script" "$log"; exit 0
fi
echo "[vmtest] RESULT: FAIL — ran=$ran not-found=$notfound missing-lib=$miss done=$(printf '%s\n' "$clean"|grep -q BBMARK_DONE && echo yes||echo no)"
echo "[vmtest] serial log: $log"
exit 1
