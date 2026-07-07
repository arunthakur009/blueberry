# Blueberry mirror kit

Everything needed to run a Blueberry bpm mirror (a read-only replica of the
origin repo) or to configure the origin itself. A mirror is an **untrusted file
server** — the index is ed25519-signed and every package is sha256-verified by
the client — so replication is a plain rsync pull with no secrets and no push
access. See the [Hosting a Mirror](../../../wiki/Hosting-a-Mirror.md) wiki page
for the walkthrough.

| File | Where it runs | Purpose |
|------|---------------|---------|
| `nginx-repo.conf` | origin **and** mirror | vhost with the correct cache policy: `.bpm` immutable (cache forever), `bpm.index`/`.sig` no-cache, `.index-backups/` hidden |
| `rsyncd-blueberry.conf` | origin | read-only rsync module `blueberry-repo` so mirrors can pull |
| `mirror-sync.sh` | mirror | three-phase pull (add packages → swap index → prune) → `/usr/local/bin/blueberry-mirror-sync` |
| `blueberry-mirror-sync.service` / `.timer` | mirror | run the sync every 15 min (jittered, persistent) |
| `mirror-setup.sh` | mirror | one-shot installer: drops the above into place + first sync |
| `mirrorlist` | reference | canonical mirror URLs for `repos.conf` |

## Origin (one-time)

```sh
cp rsyncd-blueberry.conf /etc/rsyncd.conf        # or include it
systemctl enable --now rsync
cp nginx-repo.conf /etc/nginx/sites-available/blueberry-repo   # already deployed
```

> **Exposing rsync (873):** Cloudflare only proxies HTTP, so an off-site mirror
> can't reach the module through the CDN. Either open 873 to a grey-clouded DNS
> record (publishes the origin's real IP) or pull over rsync-over-SSH with a
> command-forced key. The origin's `ufw` currently keeps 873 localhost-only, so
> the module is verified working locally but not yet publicly reachable — make
> this call when the first off-site mirror is provisioned. Alternatively, a
> mirror can sync entirely over HTTPS through the CDN (fetch `bpm.index`, then
> `GET` each `.bpm`); slower and no delta, but needs no new port.

Publishing packages goes through [`../repo-publish.sh`](../repo-publish.sh),
which re-indexes with the hardened `bpmrepo.sh` (count-floor, backups, atomic
swap).

## New mirror

```sh
ORIGIN_RSYNC=rsync://<origin>/blueberry-repo sh mirror-setup.sh --enable
```

Then add the mirror's URL to clients' `repos.conf` (nearest first); `bpm` fails
over across mirrors automatically.

## Scaling notes (forward plan)

Already in place:

- **Multiple versions per name are safe.** `bpmrepo.sh` indexes the newest
  version per package (version-aware `sort -V`), so leaving old `.bpm` in the
  pool never shadows the newer one (`bpm` resolves the first index line). Prune
  old versions only to reclaim disk.
- **Rollback/replay protection.** The signed index carries a monotonic serial;
  `bpm` refuses a mirror serving an older one, so an untrusted replica can't
  hand clients a stale index.

Not yet needed (do when there's a driver, not before — each changes the
download-URL contract for already-deployed clients, so it needs a compat story):

- **`pool/` split.** Move blobs into `pool/` and keep only metadata at the root.
  Backward compatible only if the index's filename field carries the `pool/`
  prefix (clients fetch `<url>/<filename>` verbatim). Defer until the flat pool
  is actually unwieldy.
- **Per-arch index.** Today every package is `x86_64` and the arch is in the
  filename. When `aarch64` servers appear, split into `<arch>/bpm.index` (or
  `bpm.index.<arch>`) and teach `bpm` to fetch its arch's index. No aarch64
  target exists yet, so this is deliberately deferred.
