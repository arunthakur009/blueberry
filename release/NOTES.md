## Blueberry Linux — v0.6.0-beta

**Containers land.** Blueberry can now run OCI containers with **podman 6.0.0**,
installed on demand from the mirror — no daemon, no Docker.

### Containers (podman 6.0.0)

`bpm install podman` pulls the full, self-hosted container stack:

| Package | Role |
|---------|------|
| podman 6.0.0 | daemonless container/pod engine |
| crun 1.28 | fast, low-memory OCI runtime |
| conmon 2.2.1 | per-container monitor |
| netavark 2.0.0 | container networking (bridge, port-forward) |
| aardvark-dns 2.0.0 | DNS for container names |
| containers-common 0.64.1 | default policy / registries / storage / seccomp |
| catatonit 0.2.1 | container init (`podman --init`) |

Root containers work out of the box — netavark bridge networking, kernel
overlay storage, `docker.io` as the default registry. podman is built without
gpgme/btrfs/devicemapper (pure-Go OpenPGP + overlay/vfs), so the runtime closure
stays on the existing base libraries.

```sh
bpm install podman
podman run --rm docker.io/library/alpine echo "hello from a container"
```

Rootless uplink (pasta) and fuse-overlayfs storage are the next step; today
containers run as root or via the `podman.socket` API service.

### Package manager (bpm 1.11.0) — rollback/replay protection

The signed repo index now carries a monotonic serial, and `bpm` refuses a mirror
that serves an **older** index than one it has already accepted — closing a
downgrade attack window as the mirror network grows. A failed `bpm update` (all
mirrors down, or a rejected rollback) no longer wipes your local index; it keeps
what you have and reports the failure.

### Packaging fix — no more info-file conflicts

Packages no longer ship the shared Texinfo `/usr/share/info/dir` index, which
previously caused file-conflict errors when installing two info-shipping
packages together (e.g. `readline` + `gdbm`). 25 base packages were rebuilt.

### Mirror & delivery (behind the scenes)

The package mirror was hardened this cycle: publishing can no longer clobber the
index with an empty/short one (count-floor + timestamped backups + atomic swap),
`.bpm` packages are now edge-cached immutably by the CDN while the index stays
fresh, multi-version pools are safe (newest-per-name indexing), and a read-only
rsync mirror kit is ready for downstream replicas.

---

Already on v0.5.x? `bpm update && bpm upgrade` pulls bpm 1.11.0 and the rebuilt
base packages; then `bpm install podman` to get containers.
