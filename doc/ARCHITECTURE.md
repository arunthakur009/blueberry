# Blueberry Linux — Architecture

## 1. Overview

Blueberry Linux is a monolithic-source server-focused Linux distribution. All
components that form the base operating system — the kernel, C library, core
utilities, init system, and package manager — live in a single source tree and
are built together from a top-level GNUmakefile, in the tradition of BSD
operating systems.

```
┌──────────────────────────────────────────────────────────────────────┐
│                     Blueberry Linux Source Tree                      │
│                                                                      │
│  GNUmakefile ─ top-level BSD-style build orchestrator                │
│  Make.config ─ tunable build variables (arch, jobs, versions)        │
│                                                                      │
│  src/kernel/    Linux 7.0 fetch + patch + build → vmlinuz            │
│  src/lib/musl/  musl 1.2.x → sysroot (libc.so, headers)             │
│  src/busybox/   busybox 1.36.x → /bin/busybox + applet symlinks      │
│  src/init/      runit 2.1.x → /sbin/runit-init + stage scripts      │
│  src/bpm/       bpm (Go) → /usr/bin/bpm                              │
│  src/initramfs/ initramfs /init script + build rules → initramfs.cpio│
│                                                                      │
│  etc/           /etc skeleton overlaid onto the assembled rootfs      │
│  pkgs/          BBUILD recipes for optional packages                  │
│  tools/         host-only scripts (mkiso, mkrepo, bootstrap)          │
│  doc/           this documentation                                    │
└──────────────────────────────────────────────────────────────────────┘
```

## 2. Component Relationships

```
Build host
  └─ make world
       ├─ fetch      downloads linux, musl, busybox, runit tarballs → obj/src/
       ├─ musl       builds libc sysroot → obj/sysroot/
       ├─ busybox    links against sysroot → obj/rootfs/bin/busybox
       ├─ runit      links against sysroot → obj/rootfs/sbin/runit*
       ├─ bpm        CGO_ENABLED=0 go build → obj/bpm
       ├─ kernel     builds Linux → obj/boot/vmlinuz + modules in rootfs
       └─ initramfs  packs busybox + /init → obj/boot/initramfs.cpio.zst

Runtime (booted system)
  kernel → initramfs:/init → switch_root → /sbin/runit-init
               ↑                                 ↑
        finds root device               runs stage 1, 2, 3
        optional LUKS                   stage 2 → runsvdir
        optional LVM                      supervises services
```

## 3. Design Decisions

### 3.1  musl libc instead of glibc

musl provides a correct, small, maintainable POSIX C library. It is the libc
of Alpine Linux. Key properties relevant to Blueberry:

- Single-file build, no interdependencies between musl and busybox
- Static linking is reliable and produces small binaries
- Tighter security surface (no RUNPATH injection, no LD_PRELOAD default)
- Suitable for minimal containers and embedded servers

Trade-off: some proprietary software ships glibc-only binaries. On Blueberry,
those binaries require a glibc compatibility layer (`pkgs/extra/gcompat`) or
must be rebuilt.

### 3.2  busybox for base utilities

busybox combines over 300 Unix utilities into a single binary. On a server
that needs the base system to be small and auditable, this is the right choice.
The applet configuration (`src/busybox/config`) has been tuned for server use:
init is disabled (runit handles that), GUI utilities are removed.

For full POSIX compliance on specific tools, the `pkgs/core/util-linux` and
`pkgs/core/coreutils` packages provide replacements.

### 3.3  runit as the default init

runit is not systemd. It has three stages, a supervision model, and nothing
else. Service definitions are executable shell scripts in a directory. This
makes them inspectable, editable, and version-controllable without a new
tool or language.

Blueberry supports runit as the default, with service directory compatibility
layers for s6 and OpenRC described in `doc/INIT.md`. There is no plan to
support systemd.

### 3.4  bpm as the package manager

bpm is written in Go for:
- Single static binary, no runtime dependencies
- Cross-compilation with `GOARCH` flag, no build toolchain needed on target
- Native zstd support via `github.com/klauspost/compress`
- Simple, auditable codebase (~1200 lines)

The `.bb` binary package format is a zstd-compressed tar. The repository
index is a plain-text newline-record format (`BBINDEX.zst`). Both are
intentionally simple to allow inspection without bpm itself.

### 3.5  Single source tree (BSD-style)

Unlike distributions that vendor upstream sources via a package manager
(Gentoo, Arch), Blueberry's base system is versioned together. This means:

- `git clone` gives you everything needed to build a bootable OS
- A CI job can verify the entire base on every commit
- Kernel, libc, and init are known-compatible combinations
- Security patches can be committed and shipped atomically

The trade-off is that upstream releases require a deliberate update commit,
not an automatic version bump.

## 4. Boot Sequence

```
1. BIOS/UEFI loads GRUB
2. GRUB loads vmlinuz + initramfs.cpio.zst
3. Kernel decompresses initramfs, mounts it as rootfs, runs /init
4. initramfs/init:
     a. Mounts /proc /sys /dev
     b. Parses kernel command line (root=, luks=, lvm=, etc.)
     c. Loads kernel modules if needed (mdev)
     d. Optionally opens LUKS volume
     e. Optionally activates LVM
     f. Mounts real root filesystem
     g. switch_root → real rootfs
5. /sbin/runit-init (PID 1 in real root)
6. /etc/runit/1 — stage 1: remount rw, mdev, hwclock, sysctl
7. /etc/runit/2 — stage 2: runsvdir /var/service (supervise loop)
8. Services in /var/service/ start in parallel:
     - getty-tty1 (login prompt)
     - syslogd
     - sshd (if enabled)
9. System is up
10. On shutdown: runit → /etc/runit/3 (drain services, sync, unmount, halt)
```

## 5. Package System

```
Developer writes pkgs/core/foo/BBUILD
  ↓
bpm build pkgs/core/foo/BBUILD
  → downloads source, calls build(), calls package()
  → creates foo-1.0.0-1-x86_64.bb

tools/mkrepo.sh <pkg-dir>
  → reads .MANIFEST from each .bb
  → writes BBINDEX.zst

Repository server (Nginx) serves:
  BBINDEX.zst
  foo-1.0.0-1-x86_64.bb

User runs: bpm update && bpm install foo
  → fetches BBINDEX.zst
  → resolves dependencies (BFS)
  → downloads + verifies .bb files
  → extracts into / with ownership preserved
  → records in /var/lib/bpm/db/installed/foo/
```

## 6. Security Model

- All userspace code is compiled with `-fstack-protector-strong`
- Kernel has PTI, Retpoline, KASLR, SMAP/SMEP enabled
- sysctl defaults in `etc/sysctl.d/10-blueberry.conf` enforce network
  hardening, dmesg restriction, kptr_restrict, ASLR level 2
- sshd ships with `PermitRootLogin no`, `PasswordAuthentication no`
- Packages should be signed with minisign (see `doc/HOSTING.md`)
- The root filesystem is mounted read-only by the initramfs; stage 1 
  remounts it read-write after fsck

## 7. Directory Reference

| Path | Description |
|------|-------------|
| `GNUmakefile` | Top-level build entry point |
| `Make.config` | Default build variables |
| `Make.local` | Machine-local overrides (gitignored) |
| `src/kernel/` | Linux kernel config, patches, Makefile |
| `src/lib/musl/` | musl libc build rules |
| `src/busybox/` | busybox config + Makefile |
| `src/init/` | runit stage scripts, service definitions, Makefile |
| `src/bpm/` | bpm Go source + Makefile |
| `src/initramfs/` | `/init` script + Makefile |
| `etc/` | /etc skeleton (copied to rootfs at install time) |
| `pkgs/core/` | Core BBUILD recipes (built by CI, in the repo) |
| `pkgs/extra/` | Extended BBUILD recipes |
| `pkgs/community/` | Community-maintained recipes |
| `tools/` | Host-only scripts: mkiso, mkrepo, bootstrap |
| `doc/` | All documentation |
| `obj/` | Build artefacts (gitignored) |
| `obj/src/` | Extracted upstream source tarballs |
| `obj/sysroot/` | musl libc sysroot for building userland |
| `obj/boot/` | vmlinuz, System.map, initramfs.cpio.zst |
| `obj/rootfs/` | Assembled root filesystem (DESTDIR) |
| `obj/repo/` | Built .bb packages + BBINDEX.zst |
