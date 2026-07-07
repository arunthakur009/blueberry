# Blueberry Console (web UI)

A first-party web console to manage a Blueberry server — a deliberately scoped
alternative to Cockpit/Proxmox that integrates the one thing that's uniquely
Blueberry: `bpm` + a rolling, snapshot/rollback-able base.

This document describes the **base layer** (`src/bbconsole`, package
`blueberry-console`) and the far vision it's built to grow into.

## What ships today (the base layer)

- **`bbconsole`** — a small privileged daemon (Rust, pure-std HTTP, one runtime
  dependency: `serde_json`). It wraps tools that already exist on the box —
  `systemctl`, `bpm`, `/proc` — behind a versioned, authenticated, audited JSON
  API, and serves a single-page frontend.
- **API** (`/api/v1`): `system` (host/kernel/load/memory), `services`
  (list + start/stop/restart), `packages` (via `bpm`). Far-vision areas
  (`containers`, `updates`, `logs`, `storage`, `network`) return `501` with a
  stable shape so the frontend can grow without churn.
- **Frontend** — a dependency-free SPA (`/usr/share/blueberry-console/web`):
  token login, overview dashboard, services panel, packages panel, and stub tabs
  for the roadmap.

## Security model

The console is **privileged by design** (it manages services and packages), so
the boundary is the whole game:

- **Localhost only.** `bbconsole` binds `127.0.0.1:9090`. It does **not** do TLS
  or internet exposure itself — put a reverse proxy (nginx, the same pattern the
  mirror uses) in front for TLS and access control, or reach it over an SSH
  tunnel. This keeps crypto/exposure out of the privileged daemon.
- **Token → session auth.** On first start it writes a random admin token to
  `/etc/blueberry/console/token` (mode 0600). The admin reads it as root and logs
  in; the daemon returns an `HttpOnly; SameSite=Strict` session cookie tracked in
  memory with a 1-hour idle expiry. Every API call is re-checked.
- **Small surface.** Pure-std HTTP, hard request-size limits, one request per
  connection, path-traversal guard on static files, security headers + a strict
  CSP on every response.
- **Write actions are few, validated, and audited.** Service actions accept only
  `start`/`stop`/`restart` on a validated unit name; every login and write is
  appended to `/var/log/blueberry-console/audit.log`.

Run it:

```sh
bpm install blueberry-console
systemctl enable --now blueberry-console
cat /etc/blueberry/console/token        # log in with this
# then reach http://127.0.0.1:9090 via an SSH tunnel or a TLS reverse proxy
```

## Far vision (roadmap)

Each area extends the `/api/v1` surface + adds a frontend panel; the base layer's
router, auth, and audit don't change.

1. **Containers** — podman (there's already a `podman.socket` REST API to proxy):
   list/start/stop/logs, images, pods; rootless-aware.
2. **Updates + rollback** — the differentiator. Surface `bpm` updates, and if the
   root is btrfs: snapshot → `bpm upgrade` → one-click rollback if it broke. No
   other console does this for a source-built rolling distro.
3. **Logs** — journald (`journalctl -o json`), per-unit, follow/tail.
4. **Storage** — lvm/btrfs/xfs: volumes, subvolumes, snapshots, SMART.
5. **Network** — nftables/NetworkManager: interfaces, firewall rules, wireguard.
6. **Auth v2** — PAM / per-user accounts + roles behind `login()`; TLS termination
   option; optional 2FA.

## Extending

- A new read panel = one handler in `src/bbconsole/src/api.rs` + a match arm in
  `api_route` + one entry in `PANELS` in `web/app.js`.
- Keep write actions argument-validated and audited (see `service_action`).
- Keep the daemon localhost-bound and dependency-light; push exposure/TLS to the
  proxy layer.
