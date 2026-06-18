# Blueberry → systemd migration

Blueberry historically booted a custom busybox `/init` (live, from initramfs) and
runit (installed system), with dropbear for SSH. This document tracks the
migration to **systemd as PID 1** — journald, logind, systemd-udevd,
systemd-networkd/resolved/timesyncd, `.service` units, and OpenSSH.

It is deliberately phased; each phase is independently buildable and committed.

## Design decision: opt-in, installed-system only

systemd governs the **installed disk system**; the **live initramfs stays
busybox-based**. This keeps the tiny RAM-first live CLI intact and contains the
size/complexity cost to systems that actually install to disk. The init system
is selected at build time:

```
make iso              # INIT=runit   (default) busybox + runit + dropbear
make iso INIT=systemd # full systemd PID 1 on the installed rootfs + OpenSSH
```

The single indirection point is **`/sbin/init`**: the initramfs `switch_root`s
into the disk and execs `/sbin/init`, which a runit image points at
`runit-init` and a systemd image points at `/usr/lib/systemd/systemd`. The same
kernel + initramfs boot either image; no `init=` on the kernel cmdline is
required.

## Phase 0 — prerequisites (kernel + deps)  ✅ done
- [x] Kernel config: `CONFIG_FHANDLE`, `CONFIG_AUTOFS_FS`, `CONFIG_TMPFS_XATTR`,
      `CONFIG_SECCOMP`/`SECCOMP_FILTER`, `CONFIG_KCMP`, `CONFIG_DMIID`,
      `CONFIG_CRYPTO_HMAC`, `CONFIG_CGROUP_PIDS`, plus the cgroup-v2 / namespace
      options already present. Unified cgroup hierarchy is the runtime default.
- [x] Package **dbus** 1.16.0 (system bus).
- [x] Package **util-linux** 2.40.2 — libmount/libblkid/libuuid/libfdisk/
      libsmartcols + agetty, mount, login, etc. Made the **canonical**
      libuuid/libblkid provider; e2fsprogs rebuilt `--disable-libuuid
      --disable-libblkid` and the cryptsetup/gptfdisk/wget/python/eudev deps
      repointed to util-linux.
- [x] Package **systemd** 256.7 (meson) with a Blueberry preset: journald,
      logind, udevd, networkd, resolved, timesyncd, hostnamed, localed enabled;
      homed/portabled/importd/machined/oomd/repart/sysupdate/boot/efi/pam/
      selinux/man/tests disabled. Runtime closure: glibc, libcap, util-linux,
      libseccomp, kmod, dbus, acl, xz, zstd, lz4, cryptsetup.
- [x] **shadow dropped** — util-linux ships `login`/`agetty` and busybox covers
      `passwd`; this avoids the libbsd/libmd cascade. systemd-sysusers creates the
      `systemd-network`/`systemd-resolve`/`systemd-timesync` users at first boot.
- [x] Package the netfilter libs for the existing iptables/nft stack
      (libnfnetlink, libnetfilter_conntrack).
- [x] Closure validated by `tools/check-pkg-libs.sh` (0 unsatisfiable / 0
      undeclared across the full prospective set).

## Phase 1 — installed-system init  ✅ done (boot-verification pending)
- [x] `src/systemd/` integration layer + `INIT=systemd` switch in the
      GNUmakefile. `_do_install` bakes the systemd runtime closure into the base
      image, installs the layer, and points `/sbin/init` at systemd.
- [x] Units / config shipped by the layer:
  - `systemd-networkd` + `20-wired.network` (DHCP on all wired interfaces);
    `resolv.conf` → resolved stub; resolved + timesyncd enabled.
  - OpenSSH `sshd.service` + `sshd-keygen.service` (host keys on first boot) +
    `sshd_config.d/10-blueberry.conf`; `tmpfiles.d` for the privsep dir.
  - `getty@tty1` enabled; `default.target` = `multi-user.target`.
  - The `[Install]` symlinks are created statically at image-assembly time
    (there is no running `systemctl` then).
- [x] Initramfs made init-agnostic (`exec /sbin/init`).
- [ ] **Boot-verify in QEMU**: assemble `make iso INIT=systemd`, boot, confirm
      `systemctl is-system-running`, a networkd DHCP lease, journald, and sshd.

## Phase 2 — initramfs / boot
- [x] Installer (`blueberry-install.c`): extracts the (systemd or runit) rootfs
      and writes a `root=UUID=…` grub.cfg with no `init=`, relying on the
      `/sbin/init` indirection — so the existing installer handles both images
      unchanged.
- [ ] Optional: a systemd-in-initrd live image (currently the live medium stays
      busybox; only the installed system is systemd).

## Phase 3 — swap userland defaults  ✅ done for the systemd image
- [x] OpenSSH replaces dropbear on systemd images (sshd.service + host-key gen).
- [x] journald replaces busybox syslogd; `systemctl`/`journalctl`/`networkctl`/
      `loginctl` available. The runit `sv`/`sv-enable`/`sv-disable` helpers are
      simply not installed on a systemd image.

## Phase 4 — verify + docs
- [ ] `make test` self-test variant for a systemd boot (assert `systemctl
      is-system-running` is running/degraded, sshd up, networkd lease, journald).
- [ ] Update the remaining docs (README, BUILD, INSTALL, ARCHITECTURE) once a
      systemd boot is verified end-to-end.
- [ ] Convert the package-shipped service files (chrony/redis/nginx) to
      `.service` units (today they ship runit `run` scripts; harmless on a
      systemd box but inert until ported).

## Notes / tensions
systemd reverses Blueberry's minimal/runit ethos and pulls in a meaningfully
larger base (dbus, util-linux, systemd). The monolithic, **module-less** kernel
is fine for systemd (udevd simply loads nothing), but cgroup v2 + FHANDLE are
mandatory. Image size grows notably on systemd images; that is an accepted
consequence of the "full systemd PID 1" decision. The default `INIT=runit`
image is unchanged, so the deployed minimal system stays stable.
