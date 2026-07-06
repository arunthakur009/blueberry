#!/bin/sh
# test-bpm.sh — fast, self-contained tests for the bpm package manager's
# install / upgrade / config-preservation logic. Runs against a throwaway
# BPM_ROOT (no QEMU). Builds bpm from src/bpm-rs and two versions of a synthetic
# config-shipping package, then asserts the pacman/dpkg backup behaviour and a
# basic install/remove round-trip. Exits non-zero on the first failure.
set -eu

TOP=$(cd "$(dirname "$0")/../.." && pwd)
BPM="${BPM_BIN:-}"
W=$(mktemp -d); trap 'rm -rf "$W"' EXIT
fail() { printf '  FAIL: %s\n' "$*" >&2; exit 1; }
ok()   { printf '  ok: %s\n' "$*"; }

# ── build bpm if not supplied ────────────────────────────────────────────────
if [ -z "$BPM" ]; then
    echo "[test-bpm] building bpm"
    ( cd "$TOP/src/bpm-rs" && cargo build --release >/dev/null 2>&1 ) || fail "bpm build failed"
    BPM="$TOP/src/bpm-rs/target/release/bpm"
fi
[ -x "$BPM" ] || fail "no bpm binary at $BPM"

# ── build two versions of a synthetic config package ─────────────────────────
mkrec() { # <ver> <config-content>
    mkdir -p "$W/rec/tcfg"
    cat > "$W/rec/tcfg/bpm.toml" <<EOF
[package]
name = "tcfg"
version = "$1"
release = 1
summary = "bpm test package"
arch = ["any"]
backup = ["etc/tcfg.conf"]
[steps]
package = '''
mkdir -p "\$pkgdir/etc" "\$pkgdir/usr/bin"
printf '%s\n' "$2" > "\$pkgdir/etc/tcfg.conf"
printf '#!/bin/sh\necho tcfg\n' > "\$pkgdir/usr/bin/tcfg"; chmod 755 "\$pkgdir/usr/bin/tcfg"
'''
EOF
    ( cd "$W/rec" && BPM_ARCH=x86_64 SOURCE_DATE_EPOCH=1767225600 python3 "$TOP/tools/pkg/bpmbuild" tcfg "$W/out" >/dev/null 2>&1 ) \
        || fail "bpmbuild v$1 failed"
}
mkdir -p "$W/out"
mkrec 1 "setting=default"
mkrec 2 "setting=newdefault"
V1="$W/out/tcfg-1-1-any.bpm"; V2="$W/out/tcfg-2-1-any.bpm"

echo "[test-bpm] 1/4 install + files/list"
R="$W/r1"; export BPM_ROOT="$R"; export BPM_NO_SCRIPTLETS=1
"$BPM" install "$V1" >/dev/null 2>&1 || fail "install v1"
[ -f "$R/etc/tcfg.conf" ] && [ -x "$R/usr/bin/tcfg" ] || fail "payload not installed"
"$BPM" list 2>/dev/null | grep -q '^tcfg 1' || fail "list doesn't show installed tcfg"
ok "install records files + db"

echo "[test-bpm] 2/4 EDITED config is preserved on upgrade"
echo "setting=MINE" > "$R/etc/tcfg.conf"
"$BPM" install "$V2" >/dev/null 2>&1 || fail "upgrade v2 (edited)"
[ "$(cat "$R/etc/tcfg.conf")" = "setting=MINE" ] || fail "edited config was clobbered"
[ "$(cat "$R/etc/tcfg.conf.bpmnew" 2>/dev/null)" = "setting=newdefault" ] || fail ".bpmnew not written"
ok "edit preserved, new default saved as .bpmnew"

echo "[test-bpm] 3/4 UNMODIFIED config updates cleanly"
R="$W/r2"; export BPM_ROOT="$R"
"$BPM" install "$V1" >/dev/null 2>&1 || fail "install v1 (clean)"
"$BPM" install "$V2" >/dev/null 2>&1 || fail "upgrade v2 (clean)"
[ "$(cat "$R/etc/tcfg.conf")" = "setting=newdefault" ] || fail "unmodified config not updated"
[ -e "$R/etc/tcfg.conf.bpmnew" ] && fail "spurious .bpmnew on unmodified config"
ok "unmodified config updated, no spurious .bpmnew"

echo "[test-bpm] 4/4 remove keeps user config, drops the binary"
R="$W/r1"; export BPM_ROOT="$R"
"$BPM" remove tcfg >/dev/null 2>&1 || fail "remove"
[ -e "$R/usr/bin/tcfg" ] && fail "binary not removed"
[ -f "$R/etc/tcfg.conf" ] || fail "user config was deleted on remove (should be kept)"
ok "remove drops payload, preserves edited config"

echo "[test-bpm] PASS"
