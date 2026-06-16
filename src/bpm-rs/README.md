# bpm (Rust)

The Blueberry Package Manager, rewritten in Rust. Drop-in compatible with the
C `bpm` in `../bpm`: identical on-disk database, cache and index layout, the
same repo index format and ECDSA-P256 signature scheme, and the same commands —
so the two can replace each other on a live system.

## Why the rewrite

The C version buffered an entire `.pkg.tar.zst` in RAM to extract it, so
`bpm install gcc` (≈200 MB uncompressed) hit ~246 MB RSS and was OOM-killed on
small install VMs. Here extraction streams through the `zstd` + `tar` crates
straight to disk — a 200 MB package installs in ~5 MB RSS. Memory safety and
the streaming model come for free.

## Commands

`install`/`in`, `remove`/`rm`, `update`/`up`, `upgrade`, `search`/`se`,
`list`/`ls`, `info`, `files`, `owns`. `BPM_ROOT=<dir>` installs into a staging
root (chrooting for scriptlets/ldconfig). `BPM_ALLOW_UNSIGNED` and
`BPM_NO_SCRIPTLETS` are the same escape hatches as the C build.

## Dependencies

`ureq` (rustls TLS), `zstd`, `tar`, `sha2`, `p256` (index signature). The
release binary links only libc + libgcc_s; libzstd is statically bundled.

## Build

```sh
cargo build --release        # target/release/bpm
cargo test                   # vercmp parity tests
```

Packaged for the repo by `packages/bpm/PKGBUILD` (built in the Arch container,
`makedepends=rust`). Install it on a running system with `bpm install bpm`.

The signing public key is baked into `src/sig.rs` and must match
`../bpm/repokey.h`. Rotate both together (`tools/mkrepokey.sh` for the C header).
