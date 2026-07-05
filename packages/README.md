# Blueberry packages

Native **`.bpm`** recipes (`bpm.toml`) for software that runs on Blueberry
Linux. Each recipe builds in an ephemeral Arch container (the self-hosted build
toolchain) and produces a `.bpm` payload — a `zstd(tar)` archive with a TOML
`.BPM` manifest — that installs onto a Blueberry rootfs with **`bpm`** (see
`src/bpm-rs`), the native package manager, which resolves `depends` and
sha256-verifies every download from the mirror.

> The old Arch-derived path (`PKGBUILD` / `makepkg` / `.pkg.tar.zst` / OBS) is
> **retired** — see `doc/BPM-FORMAT.md`. There are no `PKGBUILD`s left; the
> format here is `bpm.toml` exclusively. Because Blueberry is built against
> glibc, the resulting binaries are ABI-compatible with the pinned glibc/kernel
> runtime.

## Layout

```
packages/
  <name>/bpm.toml      one directory per package
```

The set spans base libraries (`zlib`, `ncurses`, `openssl`, …), userland tools
(`curl`, `nano`, `vim`, `htop`, `tmux`, `jq`, `git`), the disk/installer tools
(`e2fsprogs`, `dosfstools`, `gptfdisk`), daemons (`nginx`, `redis`, `chrony`,
`dhcpcd`), and the **compiler toolchain** (`binutils`, `gmp`, `mpfr`, `mpc`,
`gcc`). Run `ls packages/` for the current list.

## Recipe format (`bpm.toml`)

```toml
[package]
name     = "jq"
version  = "1.7.1"
release  = 1
summary  = "Command-line JSON processor"
homepage = "https://jqlang.github.io/jq/"
license  = ["MIT"]
arch     = ["x86_64"]

depends     = ["glibc", "oniguruma"]   # runtime deps (Blueberry packages)
makedepends = ["pkgconf"]              # build-only deps (from the build container)

[[source]]
url    = "https://.../jq-1.7.1.tar.gz"
sha256 = "478c9ca129fd2e3443fe27314b455e211e0d8c60bc8ff7df703873deeee580c2"

[steps]
build   = '''cd "$name-$version"; ./configure --prefix=/usr …; make'''
package = '''cd "$name-$version"; make DESTDIR="$pkgdir" install'''
```

`build`/`package` are shell snippets run against the extracted source; `$name`,
`$version`, `$pkgdir` (staging root) are provided. Build-time `makedepends`
come from the Arch build container; runtime `depends` must be satisfied by
Blueberry packages (this directory) so nothing pulls from Arch on-device. Full
spec: `doc/BPM-FORMAT.md`; authoring guide: `doc/BPM-GUIDE.md`.

> **On-device compilation works.** Install the toolchain and the dev SDK:
>
>     bpm install base-devel glibc
>
> `base-devel` pulls `gcc`, `binutils`, `make` and `linux-api-headers`; `glibc`
> (requested by name, since the base only *provides* its runtime) adds the libc
> headers + startup objects (`crt1.o`/`crti.o`/`crtn.o`) + linker scripts. After
> that, `gcc hello.c` and `g++` compile and link natively. (`linux-api-headers`
> matches the Blueberry kernel; `glibc` is the same version as the bundled
> runtime, so there's no ABI mismatch.)

## Build a `.bpm` locally

Everything is driven by `tools/build-bpm-pkg.sh`, which reads a recipe's
`depends`+`makedepends`, installs them (plus `base-devel`) into an ephemeral
Arch container, then runs `bpmbuild`. It is idempotent — a package whose `.bpm`
is newer than its `bpm.toml` is skipped.

```sh
# build one (or several) packages into ./obj/bpm-out/
tools/build-bpm-pkg.sh obj/bpm-out jq

# build every recipe (the whole set)
make repo-build            # -> obj/bpm-out/*.bpm
```

`ENGINE=podman|docker` and `IMAGE=<arch image>` override the container backend.
To build a single recipe without the container wrapper (on an Arch host with the
deps already present): `tools/bpmbuild packages/jq obj/bpm-out`.

Before building, `make check-closure` (or `python3 tools/check-closure.py`)
verifies every recipe's `depends` resolve inside this directory — a closed
dependency graph, so nothing reaches for an Arch repo at install time.

## Index + publish to the mirror

`tools/bpmrepo.sh` turns a directory of `.bpm` files into the signed repository
index that `bpm` consumes:

```sh
tools/bpmrepo.sh obj/bpm-out       # writes bpm.index + ed25519 signature
```

Index line format: `name|version|filename|sha256|deps|size|desc`. The signing
key is `$BPM_SIGN_KEY` (default `~/.config/bpm/repo-ed25519.pem`). This is the
**only** indexer — do not point a `.pkg.tar.zst` indexer at the repo (it writes
an empty index and clobbers it). Deploy of the built `.bpm`s + re-index onto the
live mirror (`repo.mmzsigmond.me`) is covered in `doc/DEPLOY.md`.

> Release ISOs are **not** hosted on the mirror — it serves only `.bpm` packages
> plus the pinned kernel/glibc artifacts.

## Install onto Blueberry

Once published, `bpm` installs from the mirror, resolving deps and verifying
each payload's sha256 against the signed index:

```sh
bpm update              # refresh the index from the mirror
bpm install jq          # resolve deps + install
bpm list                # what's installed
bpm remove jq
```

For bootstrapping without `bpm`, a `.bpm` is a `zstd`-compressed tar rooted at
`/` plus a `.BPM` manifest member; extract the payload onto the rootfs and drop
the manifest:

```sh
tar --zstd -xf jq-1.7.1-1-x86_64.bpm -C /path/to/blueberry/rootfs --exclude='.BPM'
```

Make sure each package's `depends` are present on the target too.

## Updating a package

1. Bump `version` (reset `release = 1`).
2. Update the `[[source]]` `url` if the path encodes the version.
3. Refresh the checksum:
   ```sh
   curl -fsSL <new-source-url> | sha256sum
   ```
   and paste it into `sha256 = "…"`.
4. Rebuild (`tools/build-bpm-pkg.sh obj/bpm-out <name>`) to confirm, then commit.
