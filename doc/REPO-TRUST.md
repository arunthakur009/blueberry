# Repository trust boundary: core (bpm) vs BUR

Blueberry has two package channels with **different trust models**. Keeping them
separate is a security invariant — don't let them blur as either grows.

## The two channels

| | **core** | **BUR** |
|---|---|---|
| What | Official Blueberry packages | Community recipes |
| Form | Prebuilt `.bpm` binaries | `bpm.toml` recipes (source) |
| Trust | Project-signed (ed25519), SHA-256 per package | *You* review the recipe and build it yourself |
| Delivery | The bpm mirror (`repo.blueberrylinux.org` + replicas) | The BUR web app (`bur.blueberrylinux.org`) |
| Analogy | A distro's official repo | The AUR |

**core** is a set of binaries we vouch for: the signed index authenticates every
package (see [SECURITY.md](SECURITY.md) and
[Hosting a Mirror](../wiki/Hosting-a-Mirror.md)). **BUR** is source recipes the
community submits; a user reads the recipe and builds it locally. BUR ships **no
binaries**, so it needs no signing key and no binary mirror — the trust is the
user's own review + build, exactly like the AUR.

## The invariant

> A community channel must never be able to deliver a binary that a client
> trusts with the same authority as an official one.

Today this holds trivially because BUR is recipes only. Two ways it could be
violated as things grow — both forbidden:

1. **Signing BUR content with the core key.** Never. The core ed25519 key
   (`repokey.rs` `REPO_PUBKEY`) authenticates official binaries only. A package
   built from a community recipe must not be signed by it.
2. **Serving community binaries from the core mirror.** Never. The core mirror
   (`/srv/blueberry-repo`) carries official `.bpm` only.

## If a BUR binary cache is ever built (Chaotic-AUR style)

Popular BUR recipes could one day be prebuilt into a convenience cache. That is
allowed **only** as a fully separate, opt-in repo:

- **Separate repo, separate key.** Its own signing keypair, distinct from the
  core key. A user opts in by adding a `bur` line to `/etc/bpm/repos.conf`:

  ```
  core https://repo.blueberrylinux.org https://<mirror> ...
  bur  https://bur-cache.blueberrylinux.org
  ```

- **Prerequisite — per-repo keys in bpm.** `sig::verify_index` currently checks
  *every* repo's index against the single baked `REPO_PUBKEY`, so a second repo
  can't yet have its own key. Before a BUR binary cache can exist, bpm must gain
  per-repo trust anchors — e.g. an optional pubkey per `repos.conf` line, or a
  keyring dir (`/etc/bpm/keys/<repo>.ed25519`), with `verify_index` taking the
  repo's key instead of the global one. Adding the `bur` repo would then require
  the user to also install its key, making the trust decision explicit. This is
  **deferred** — not built until a BUR binary cache is actually on the table,
  because it touches the security-critical verification path and has no consumer
  today.

## Operational scaling — they scale differently

- **The core mirror is static files.** It replicates trivially: read-only rsync
  pull, any number of untrusted replicas, signed index + per-package SHA-256 do
  the rest. See the [mirror kit](../tools/release/mirror/).
- **BUR is a DB-backed web app** (Next.js + Prisma). You do **not** rsync-mirror
  it. Front it with a CDN (Cloudflare) for the read-heavy pages; if it ever
  needs geographic scale, that's database read-replicas, not file replication.
  A stale file replica of a dynamic app would serve wrong data.

Keeping the static, signed, replicable **core** distinct from the dynamic,
review-based **BUR** is what lets each scale without weakening the other.
