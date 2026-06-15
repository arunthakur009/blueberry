# Blueberry packages

PKGBUILD recipes (Arch `makepkg` format) for software that runs on Blueberry
Linux. Because Blueberry is now built against **glibc**, standard Arch glibc
binaries run on it — so these build with the normal Arch toolchain and the
resulting `.pkg.tar.zst` payloads drop straight onto a Blueberry rootfs.

> Blueberry has a native package manager now — **`bpm`** (see `src/bpm`). It
> installs these `.pkg.tar.zst` payloads and resolves their `depends`. You can
> still extract a payload by hand (see below) for bootstrapping.

## Layout

```
packages/
  <name>/PKGBUILD      one directory per package
```

The set spans base libraries (`zlib`, `ncurses`, `openssl`, …), userland tools
(`curl`, `nano`, `vim`, `htop`, `tmux`, `jq`, `git`), the disk/installer tools
(`e2fsprogs`, `dosfstools`, `gptfdisk`), and the **compiler toolchain**
(`binutils`, `gmp`, `mpfr`, `mpc`, `gcc`). Run `ls packages/` for the current
list.

> **On-device compilation (follow-on).** `gcc`/`binutils` install and run, but
> compiling C on-device also needs a *dev-SDK layer* that isn't packaged yet:
> the Linux API headers, and glibc's headers + startup objects (`crt1.o`,
> `crti.o`, `crtn.o`) + linker scripts. Blueberry currently bundles only the
> glibc *runtime* (via `tools/bundle-glibc.sh`). Until a `linux-api-headers`
> and a glibc `-dev` package land, `gcc hello.c` will fail at `#include
> <stdio.h>`. The compiler packages themselves are complete.

Build-time `makedepends` are resolved from the Arch build container; runtime
`depends` are satisfied by Blueberry packages (this directory) so nothing pulls
from Arch on-device.

## Build one locally (on Arch, for testing)

```sh
cd packages/jq
makepkg -si          # build + install into the host (test it)
# or just build the package without installing:
makepkg -f           # -> jq-1.7.1-1-x86_64.pkg.tar.zst
```

`makepkg --verifysource` downloads the sources and checks the `sha256sums`
without building — handy for validating a recipe.

## Build with OBS on an Arch host

[Open Build Service](https://openbuildservice.org/) builds Arch packages via its
`Arch` repository type. Outline:

1. **Create a project** (once) with an Arch repository. In the project's
   `prjconf`/meta, enable the Arch build type and point at an Arch package
   mirror for the build root, e.g.:
   ```
   Type: arch
   Repotype: arch
   ```
   and a repository like `core`/`extra` from a mirror.

2. **Add a package** and put its `PKGBUILD` in the OBS package directory.
   OBS builds the recipe in a clean Arch chroot, resolving `depends`/
   `makedepends` from the configured Arch repos.

3. **Sources.** Either commit the upstream tarball alongside the `PKGBUILD`, or
   add a `_service` that downloads it at build time, e.g.:
   ```xml
   <services>
     <service name="download_url" mode="localonly">
       <param name="url">https://.../foo-1.0.tar.xz</param>
     </service>
   </services>
   ```
   The `sha256sums` in the PKGBUILD must match.

4. **Build:**
   ```sh
   osc co <project> <package>
   cp PKGBUILD <project>/<package>/
   osc add PKGBUILD
   osc commit -m "add foo"          # triggers a build
   osc results                      # watch status
   osc getbinaries <project> <package> <repo> x86_64   # fetch the .pkg.tar.zst
   ```

(For a local OBS build without the server: `osc build arch x86_64 PKGBUILD`.)

## Install onto Blueberry (until the package manager lands)

A `.pkg.tar.zst` is just a tarball with a payload rooted at `/`, plus the
metadata files `.PKGINFO`, `.MTREE`, `.BUILDINFO`, `.INSTALL`. Extract the
payload onto the rootfs and drop the metadata:

```sh
tar --zstd -xf jq-1.7.1-1-x86_64.pkg.tar.zst -C /path/to/blueberry/rootfs \
    --exclude='.PKGINFO' --exclude='.MTREE' --exclude='.BUILDINFO' \
    --exclude='.INSTALL'
```

Then rebuild the initramfs/image (or copy into a running live system). Because
the binaries are glibc and Blueberry bundles the glibc runtime + `ld.so.cache`,
they run as-is. Make sure each package's `depends` are present on the target too.

## Updating a package

1. Bump `pkgver` (reset `pkgrel=1`).
2. Update the `source` URL if the path encodes the version.
3. Refresh the checksum:
   ```sh
   curl -fsSL <new-source-url> | sha256sum
   ```
   and paste it into `sha256sums=()`.
4. `makepkg --verifysource` to confirm, then commit.
