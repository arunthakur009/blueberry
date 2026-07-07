# Blueberry base-distro roadmap

Scope: the **operating system itself** — packages, init, boot, first-run — not
the mirror/BUR infrastructure around it (that work is tracked separately and is
in good shape as of 2026-07-07). Ordered by value ÷ effort. Each item lists why
it matters, rough effort, and the main risk.

## Where the base stands today (2026-07-07)

Already a real server base, not a skeleton:

- **175 packages** including a full storage stack (btrfs/xfs/ext4, lvm2, mdadm,
  cryptsetup, smartmontools), networking (NetworkManager, nftables, ufw,
  wireguard, dhcpcd, bind-tools + diagnostics), servers/DBs (nginx, postgresql,
  mariadb, redis, openssh — all with working systemd units), and TLS/ops
  (openssl, ca-certificates, dehydrated/ACME, chrony, logrotate, dcron, rsync,
  rclone, git).
- **systemd** is the default init for installed disk systems; **runit** is the
  RAM-first live path.
- A **Rust installer** (TUI/CLI/unattended) writes a persistent system to disk.

The gaps below are about *capability*, *confidence*, and *out-of-box posture* —
not missing fundamentals.

---

## Tier 1 — Highest value

### 1.1 Container runtime (podman)
**Why:** The single biggest missing capability. A modern server OS is expected
to run containers; today Blueberry can't. Rootless, daemonless podman fits the
distro's ethos (no big daemon, no root) far better than docker.
**What:** New recipes for podman + its closure: `crun`, `conmon`, `netavark`,
`aardvark-dns`, `slirp4netns`, `catatonit`, plus `containers-common` config.
Wire `/etc/containers/` defaults and subuid/subgid for rootless.
**Effort:** Medium-high — ~7 new recipes, several are Rust/C with their own deps.
Build each through `bbdev` (build + closure) before publishing.
**Risk:** Medium — rootless networking (netavark/slirp4netns) and cgroup v2
delegation are the usual breakage points; needs a real `podman run` smoke test
on a booted system, not just a successful build.

### 1.2 Verify the installed systemd system boots-and-serves
**Why:** `INIT=systemd` is the *default*, but no test confirms a systemd-booted
installed disk reaches `multi-user.target` with services up. This is exactly the
bug class the image-ownership fix caught last cycle (setuid binaries owned by
uid 1000 broke mount/su at boot). Everything else in this roadmap sits on top of
this assumption.
**What:** `make world INIT=systemd` → install to a disk image → boot in QEMU →
assert: `systemctl is-system-running` degraded-or-running, sshd/network/chrony
active, no genuinely-failed units. Fold it into the e2e harness as a gate.
**Effort:** Low-medium — mostly wiring an automated boot assertion; the pieces
(disk image, QEMU, service-health check from `test-install.sh`) already exist.
**Risk:** Low to run; may *surface* real boot bugs (which is the point).

---

## Tier 2 — Makes a fresh install actually deployable

### 2.1 First-boot & security defaults
**Why:** The difference between "it boots" and "safe to put on the internet."
Today the image ships `root:blueberry` and it's unclear whether ufw is enabled or
sshd is hardened. A server distro should be safe by default.
**What:** ufw default-deny + allow SSH, enabled by preset; sshd hardened
(no root password login, key-friendly); first-boot forces the root password to
be changed; hostname / SSH host-key / optional authorized_keys provisioning
(a cloud-init-lite oneshot unit). Audit which services auto-enable via preset.
**Effort:** Medium — mostly unit presets + a first-boot oneshot + doc.
**Risk:** Medium — a too-aggressive default (e.g. locking SSH before a key is
set) can brick remote installs; needs the Tier-1.2 boot test to validate.

### 2.2 Observability agent
**Why:** Servers need metrics. No `node_exporter`/equivalent exists.
**What:** Package `prometheus-node-exporter` (Go, static-ish) with a systemd
unit, opt-in enable. Optional: a lightweight log shipping story.
**Effort:** Low-medium — one Go recipe + unit.
**Risk:** Low.

---

## Tier 3 — Depth & ecosystem (demand-driven)

### 3.1 More language runtimes
node, go, ruby, a JDK — driven by what BUR recipe submissions actually need.
Keep core lean; let BUR surface real demand before adding weight. **Effort:**
per-runtime medium. **Risk:** low but each adds maintenance + closure surface.

### 3.2 Backup tooling
`restic` or `borg` for real backups (rsync/rclone only half-cover it).
**Effort:** low-medium. **Risk:** low.

### 3.3 Package breadth & the BUR feedback loop
Use BUR as the demand signal for what to promote into core. Establish the
"popular BUR recipe → vetted → core package" pipeline (ties into the core/BUR
trust boundary in [REPO-TRUST.md](REPO-TRUST.md)). **Effort:** ongoing.

### 3.4 Persistent network configuration UX
NetworkManager is present; make static IP / DNS / hostname configuration a
first-class, documented flow (installer + post-install), beyond DHCP defaults.
**Effort:** low-medium. **Risk:** low.

---

## Suggested sequence

1. **1.2 boot-verify** first — cheap, and it de-risks everything else. Don't
   pile features on an unverified boot.
2. **1.1 podman** — the flagship capability add.
3. **2.1 first-boot/security** — make installs deployable, validated by (1).
4. Then Tier 2/3 as demand dictates, with BUR feeding the priority list.

## Cross-cutting: every change goes through the gates

Build each new recipe with `bbdev` (build + dependency-closure) before
publishing; run the e2e/boot check before cutting a beta. New daemons ship a
systemd unit (and a runit `sv/` only if they're relevant to the live path).
Publish via `tools/release/repo-publish.sh` (hardened, backed-up index).
