# Hosting the Package Repository and Build Server

Source code lives on **GitHub**. CI/CD runs on **GitHub Actions**. The only
server you need to run yourself is an **Nginx** instance to serve the `.bb`
package files that `bpm` downloads.

TLS is handled by a **Cloudflare Tunnel** — Cloudflare terminates HTTPS
publicly and forwards plain HTTP to Nginx on your local machine. No certs,
no open ports required.

---

## 1. Architecture Overview

```
GitHub (source code + CI)
  │
  ├─ every push / PR
  │    software.yml:  lint → test → build bpm
  │
  ├─ push to main when pkgs/** changes
  │    packages.yml:  build packages → sign → rsync to repo server
  │
  ├─ manual / weekly (workflow_dispatch / cron)
  │    world.yml:     build world → QEMU smoke tests
  │    auto-update.yml: check upstream versions → open PRs
  │
  └─ NAS / VPS running Nginx
       Cloudflare Tunnel → bb.mmzsigmond.me      → .bb files + BBINDEX.zst
```

---

## 2. Workflow Overview

### `software.yml` — bpm CI (every push / PR)

| Job | What it does |
|-----|--------------|
| `lint` | `go fmt ./...` and `go vet ./...` |
| `test` | `go test -race ./...` |
| `build` | `make bpm`, smoke-tests the binary, uploads artifact |

### `packages.yml` — package repo (push to main when recipes change)

| Job | What it does |
|-----|--------------|
| `build` | `make repo` — builds all packages with host musl-gcc (no world build needed) |
| `publish` | Signs with minisign, rsyncs to repo server |

**Key point:** `make repo` runs entirely independently of `make world`. Each
BBUILD compiles its package with the host's `musl-gcc` (from `musl-tools`).
No kernel compilation is needed.

### `world.yml` — full OS build + QEMU smoke test (manual / weekly)

| Job | What it does |
|-----|--------------|
| `build-world` | `make world JOBS=$(nproc)` — kernel + sysroot + initramfs |
| `build-packages` | `make repo` — packages needed for smoke test |
| `smoke-test` | Injects `test-init` into initramfs, boots in QEMU, verifies all four checks |

QEMU smoke tests verify:
1. Kernel boots, filesystems mount
2. Basic shell commands work (`ls`, `ps`, `mount`, `uname`)
3. Network comes up (QEMU user networking + DHCP)
4. `bpm update` and `bpm install zlib` succeed from CI-local repo

### `auto-update.yml` — upstream version check (weekly Monday 06:00 UTC)

Runs `tools/check-updates.sh --pr`. For each BBUILD with a configured upstream,
bumps `version=` and `checksums=`, creates a branch, and opens a **draft PR**
for review.

---

## 3. Repo Server Setup (Nginx)

### Directory layout on the host

```
/srv/blueberry/
  docker-compose.yml
  nginx/conf.d/
    repo.conf
  repo/
    packages/
      x86_64/      ← .bb + BBINDEX.zst + .minisig files land here via rsync
      aarch64/
    keys/
      blueberry-repo.pub   ← public signing key (served to clients)
```

### docker-compose.yml

Nginx only listens on HTTP — Cloudflare Tunnel handles TLS publicly.

```yaml
services:
  nginx:
    image: nginx:1.27-alpine
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:80"
    volumes:
      - ./nginx/conf.d:/etc/nginx/conf.d:ro
      - ./repo:/srv/repo:ro

  cloudflared:
    image: cloudflare/cloudflared:latest
    restart: unless-stopped
    command: tunnel --no-autoupdate run
    environment:
      - TUNNEL_TOKEN=${CLOUDFLARE_TUNNEL_TOKEN}
    depends_on:
      - nginx
```

### nginx/conf.d/repo.conf

```nginx
server {
    listen 80;
    server_name bb.mmzsigmond.me;

    root /srv/repo;
    autoindex on;

    location ~* BBINDEX\.zst$ {
        add_header Cache-Control "no-cache, must-revalidate";
        expires 0;
    }

    location ~* \.(bb|minisig|pub)$ {
        add_header Cache-Control "public, max-age=86400";
        expires 1d;
    }

    # Keys directory: serve files directly but suppress directory listing
    location /keys/ {
        autoindex off;
    }

    access_log /var/log/nginx/repo.access.log;
    error_log  /var/log/nginx/repo.error.log;
}
```

### .env file

```sh
CLOUDFLARE_TUNNEL_TOKEN=your-token-here
```

### Cloudflare Tunnel setup

1. Go to Cloudflare Zero Trust → Networks → Tunnels → Create a tunnel
2. Name it `blueberry`
3. Copy the tunnel token → put it in `.env`
4. In **Public Hostnames**, add:

   | Subdomain | Domain | Service |
   |-----------|--------|---------|
   | _(empty)_ | `bb.mmzsigmond.me` | `http://nginx:80` |

5. Save, then: `docker compose up -d`

---

## 4. Deploy User for rsync

```sh
# On the repo server
useradd -r -s /sbin/nologin -d /srv/blueberry/repo deploy
mkdir -p /home/deploy/.ssh && chmod 700 /home/deploy/.ssh
echo "ssh-ed25519 AAAA... github-actions" > /home/deploy/.ssh/authorized_keys
chmod 600 /home/deploy/.ssh/authorized_keys
chown -R deploy:deploy /home/deploy/.ssh /srv/blueberry/repo
```

Generate the key pair locally:

```sh
ssh-keygen -t ed25519 -C "github-actions-deploy" -f ./deploy_key -N ""
# deploy_key      → add as REPO_SSH_KEY secret in GitHub
# deploy_key.pub  → paste into authorized_keys on the server
```

---

## 5. GitHub Actions Secrets

**Settings → Secrets and variables → Actions → New repository secret:**

| Secret | Value |
|--------|-------|
| `MINISIGN_PRIVATE_KEY` | Contents of `blueberry-repo.key` (minisign signing key) |
| `REPO_SSH_KEY` | Contents of `deploy_key` (SSH private key for rsync) |
| `TAILSCALE_AUTHKEY` | Tailscale auth key — only if your server has no public IP |

---

## 6. Tailscale (private network)

If your repo server is a home NAS with no public IP:

```sh
# On the repo server
curl -fsSL https://tailscale.com/install.sh | sh
tailscale up
```

Note the Tailscale IP (e.g. `100.98.169.26`). That IP is already set in
`packages.yml`. Generate a reusable ephemeral auth key at
tailscale.com/admin/settings/keys and add it as `TAILSCALE_AUTHKEY`.

---

## 7. Package Signing

```sh
# On a secure machine
minisign -G -s blueberry-repo.key -p blueberry-repo.pub \
    -c "Blueberry Linux package repository"
```

- `blueberry-repo.key` → GitHub secret `MINISIGN_PRIVATE_KEY`
- `blueberry-repo.pub` → copy to `/srv/blueberry/repo/keys/` so clients can fetch it

Add the key on a Blueberry install:

```sh
mkdir -p /etc/bpm/trusted-keys
wget -O /etc/bpm/trusted-keys/blueberry-repo.pub \
    https://bb.mmzsigmond.me/keys/blueberry-repo.pub
```

---

## 8. Local Package Repository (Testing)

```sh
make repo
# packages land in ../blueberry-build/repo/

python3 -m http.server 8080 --directory ../blueberry-build/repo/
```

Add to `/etc/bpm/repos.d/local.conf` on target systems:

```
name    = "local"
url     = "http://192.168.1.x:8080"
enabled = true
```

---

## 9. QEMU Smoke Test (local)

```sh
# Boot with the test init (no real root needed):
qemu-system-x86_64 \
  -kernel ../blueberry-build/boot/vmlinuz \
  -initrd ../blueberry-build/boot/initramfs.cpio.zst \
  -append "console=ttyS0 init=/test-init BPMREPO=http://10.0.2.2:8080" \
  -nographic -no-reboot -m 512M \
  -net nic,model=virtio -net user
```

`test-init` is in `src/initramfs/test-init`. The CI workflow injects it
into the initramfs at test time. It's also included in the production initramfs
but is only activated when `init=/test-init` is on the kernel command line.

---

## 10. Automated Package Updates

`tools/check-updates.sh` checks upstream versions and opens draft PRs:

```sh
# Dry-run report only
tools/check-updates.sh

# Open GitHub PRs for each found update
tools/check-updates.sh --pr
```

The `auto-update.yml` workflow runs this weekly. Packages with upstream
checks configured:

| Package | Upstream check |
|---------|----------------|
| musl | musl.libc.org/releases/ |
| busybox | busybox.net/downloads/ |
| linux-headers | kernel.org/releases.json |
| openssl | github.com/openssl/openssl releases |
| util-linux | github.com/util-linux/util-linux releases |
| zlib | github.com/madler/zlib releases |
| openssh | cdn.openbsd.org/pub/OpenBSD/OpenSSH/portable/ |

---

## 11. Disaster Recovery

### Repo server goes down

Users can install from local `.bb` files: `bpm install --file package.bb`

The entire package set rebuilds from source with `make repo`.

### Signing key compromised

1. Generate a new key pair.
2. Update `MINISIGN_PRIVATE_KEY` secret in GitHub.
3. Replace `blueberry-repo.pub` on the server with the new public key.
4. Remove old key from `/etc/bpm/trusted-keys/` on all systems.
5. Rebuild and re-sign all packages by pushing to main.
6. Announce via a GitHub release / security advisory.
