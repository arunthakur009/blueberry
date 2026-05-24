# Blueberry Linux

A minimal, server-focused Linux distribution built on Linux 7.0 with a
philosophy of simplicity, security, and init freedom.

## Design Principles

- **musl libc** — small, correct, fast
- **busybox** — single binary userland
- **runit** — default init (swap freely for s6, OpenRC, dinit)
- **bpm** — Blueberry Package Manager, binary `.bb` packages
- **No systemd** — ever

## Directory Layout

```
tools/bpm/       Package manager source (Go)
pkgs/            BBUILD recipes
  core/          Base system packages
  extra/         Extended packages
  community/     Community-maintained packages
init/            Init system scripts
  runit/         runit stage scripts + service dirs
  openrc/        OpenRC services
  s6/            s6-rc services
kernel/          Kernel config and patches
build/           Bootstrap and image build scripts
rootfs/          Base filesystem skeleton
```

## Quick Start

### Build bpm

```sh
cd tools/bpm
go build -o bpm .
sudo install -m755 bpm /usr/local/bin/bpm
```

### Bootstrap a rootfs

```sh
./build/bootstrap.sh /mnt/blueberry
```

### Build a package

```sh
bpm build pkgs/core/busybox/BBUILD
```

## Package Format

Binary packages use the `.bb` extension — a zstd-compressed tar archive:

```
package-1.0.0-1-x86_64.bb
  .MANIFEST       # TOML metadata
  .CHECKSUMS      # sha256 per installed file
  .SCRIPTS/       # optional lifecycle hooks
    pre-install
    post-install
    pre-remove
    post-remove
  usr/bin/foo
  usr/lib/...
  etc/...
```

## Init Structure (runit)

```
/etc/runit/1     stage 1: mounts, hostname, seed RNG
/etc/runit/2     stage 2: runsvdir /var/service
/etc/runit/3     stage 3: shutdown

/etc/sv/<name>/  service definition
  run            executable run script
  finish         optional cleanup script
  log/run        optional log service

/var/service/    symlinks to active /etc/sv/<name>/ dirs
```

## Repositories

Configure at `/etc/bpm/repos.d/*.conf`:

```toml
name    = "core"
url     = "https://repo.blueberry.linux/packages/x86_64"
enabled = true
```

## License

MIT
