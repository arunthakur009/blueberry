## Blueberry Linux — v0.6.1-beta

A base-image correctness release on top of v0.6.0-beta (containers/podman).
Same packages and mirror — this rebuilds the ISOs to fix how the base rootfs is
assembled.

### Fix — setuid binaries work again

The base rootfs is extracted from `.bpm` packages by an unprivileged build user,
and the extraction dropped setuid/setgid mode bits — so `sudo`, `su`, `mount`
and `umount` shipped as `0755` instead of `4755` and **could not elevate for
non-root users**. The packages themselves were always correct; only the baked
image lost the bits. Both build-time extractors now preserve them (`tar -xp`),
and the image step still forces root ownership — so these land as proper
`-rwsr-xr-x root root`.

> Upgrading an existing v0.6.0 install in place doesn't re-bake these; boot from
> a v0.6.1 image, or run `bpm install -f sudo util-linux` to correct them.

### Fix — missing base directories

`/var/tmp` (1777) and `/var/cache` are now created in the base rootfs (the FHS
directory list had omitted them).

### Unchanged from v0.6.0

Containers via `bpm install podman` (podman 6.0.0 + crun/conmon/netavark/
aardvark-dns/containers-common/catatonit), bpm 1.11.0 rollback/replay
protection, and the repo-wide `info/dir` packaging fix all carry forward — no
`bpm update` needed for those, they're already on the mirror.
