## Blueberry Linux — v0.5.0-beta

The "maintainable base" release: Blueberry moves to a Debian-style **LTS kernel**
and a **bpm-upgradable base**, so an installed system can take security updates in
place instead of being a frozen snapshot.

### Kernel — now 6.18 LTS
- Pinned to the **6.18 LTS** line (was chasing latest-stable). Security comes from
  LTS point releases, which backport upstream fixes — no patchset to maintain.
- **KSPP hardening baseline** in the config: STRICT_KERNEL_RWX, HARDENED_USERCOPY,
  INIT_ON_FREE, SLAB_FREELIST_HARDENED, RANDOM_KMALLOC_CACHES, LIST_HARDENED,
  ZERO_CALL_USED_REGS, VMAP_STACK, DMESG_RESTRICT, and more.
- `uname -r` is now a clean `6.18.38-blueberry` (fixed a doubled-suffix bug).

### The whole base is `bpm`-tracked
- Every base package **and** the kernel are recorded in the bpm database at build
  time, so `bpm list` shows the full system and **`bpm upgrade` maintains it** —
  not just packages you install later.
- Kernel upgrades are now dead simple: the `linux` package overwrites
  `/boot/vmlinuz` and the existing UUID-based grub.cfg boots it. (The previous
  hook / fallback / grub-regeneration machinery was removed as unnecessary.)

### Security updates (delivered via `bpm upgrade`)
- In the base image: **openssl** 3.4.0 → 3.4.6, **sudo** 1.9.16p2 → 1.9.17p2
  (mid-2025 chroot local-priv-esc fix), **expat** 2.6.4 → 2.8.2 (13 CVEs).
- Refreshed on the repo: **curl** 8.21.0, **xz** 5.8.3, **gnutls** 3.8.13,
  **sqlite** 3.53.3, **postgresql** 17.10.

### Housekeeping
- `tools/` reorganized into role subdirectories (pkg / kernel / image / test /
  release / build); dropped a dead publisher script.

### Images

| image | what it is |
|---|---|
| `blueberry-<date>-x86_64.iso` | Installer / rescue ISO (carries the install payload) |
| `blueberry-server-x86_64.iso` | Live systemd Server (CLI) ISO |

Both are hybrid BIOS + UEFI. Write to USB: `dd if=<iso> of=/dev/sdX bs=4M oflag=sync`.

Verified end-to-end in QEMU: boots to `6.18.38-blueberry`, unattended install +
boot of the installed disk, and a rooted `bpm upgrade` pulling the new kernel +
security packages.
