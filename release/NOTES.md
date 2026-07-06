## Blueberry Linux — v0.5.2-beta

A point release on top of v0.5.1-beta, shipping **bpm 1.10.0** with safe config
upgrades, an ACME client, and security bumps across the network-facing stack.

### Package manager (bpm 1.10.0) — config files are no longer clobbered
`bpm upgrade` now preserves edited `/etc` config files instead of overwriting
them, using the pacman/dpkg model:

- Recipes declare a `backup = [...]` list. bpm records the packaged checksum of
  each config at install time.
- On upgrade it does a three-way compare: if you never touched the file it's
  updated in place; if you edited it, **your version is kept** and the new
  default is written alongside as `*.bpmnew` (with a notice) for you to merge.
- `bpm remove` leaves edited configs in place rather than deleting them.

This closes the biggest gap in running a long-lived server on a rolling distro:
`bpm upgrade` can now maintain the whole system without eating your `sshd_config`
or `nginx.conf`.

### New: ACME / Let's Encrypt client
- **`dehydrated`** — a small, auditable Bash ACME client for issuing and renewing
  TLS certificates (`bpm install dehydrated`). Rounds out the existing crypto
  stack (openssl 3.4.6, gnutls, nss, ca-certificates, openssh).

### Security bumps
- **openssh 9.9p2 → 10.3p1** — `sshd_config` / `ssh_config` are now `backup`
  configs, preserved across upgrades.
- **nginx 1.26.3 (EOL) → 1.28.3** — `nginx.conf` preserved across upgrades.

### New: bbdev — repository developer tool
- **`bbdev`** detects which package recipes you changed under `packages/`, builds
  just those `.bpm`s, and runs the dependency-closure check — one command instead
  of remembering the build invocations. Install it on a Blueberry box with
  `bpm install bbdev`, or on Arch/Debian/Fedora with the cross-distro `install.sh`.

### Testing
- New `make test-bpm` (fast, no-QEMU) covers the package manager's update path —
  install, config preservation on upgrade, and remove.
- The end-to-end install test now asserts **service health**: after install the
  system must reach multi-user with **sshd + networkd up and no failed units**.

### Images

| image | what it is |
|---|---|
| `blueberry-<date>-x86_64.iso` | Installer / rescue ISO (carries the install payload) |
| `blueberry-server-x86_64.iso` | Live systemd Server (CLI) ISO |

Both are hybrid BIOS + UEFI. Write to USB: `dd if=<iso> of=/dev/sdX bs=4M oflag=sync`.

Already on v0.5.1? `bpm update && bpm upgrade` pulls bpm 1.10.0 and the updated
packages without reinstalling.
