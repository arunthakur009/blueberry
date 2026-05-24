# Hosting the Package Repository and Build Server

Source code lives on **GitHub**. CI/CD runs on **GitHub Actions**. The only
server you need to run yourself is an **Nginx** instance to serve the `.bb`
package files that `bpm` downloads.

---

## 1. Architecture Overview

```
GitHub (source code + CI)
  │
  ├─ push / PR  → GitHub Actions
  │                 lint → test → build bpm
  │                 (main only) → build world → build packages
  │                             → sign with minisign
  │                             → rsync to repo server
  │
  └─ repo server (your NAS / VPS running Nginx)
       repo.blueberry.linux  ── .bb files + BBINDEX.zst
```

---

## 2. Repo Server Setup (Nginx on your NAS)

The repo server is a single Nginx container. No Forgejo, no Woodpecker.

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
  certbot/
    conf/
    webroot/
```

### docker-compose.yml

```yaml
# /srv/blueberry/docker-compose.yml
services:
  nginx:
    image: nginx:1.27-alpine
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./nginx/conf.d:/etc/nginx/conf.d:ro
      - ./repo:/srv/repo:ro
      - ./certbot/conf:/etc/letsencrypt:ro
      - ./certbot/webroot:/var/www/certbot:ro
```

### nginx/conf.d/repo.conf

```nginx
server {
    listen 443 ssl http2;
    server_name repo.blueberry.linux;

    ssl_certificate     /etc/letsencrypt/live/repo.blueberry.linux/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/repo.blueberry.linux/privkey.pem;
    ssl_protocols       TLSv1.3 TLSv1.2;
    ssl_ciphers         HIGH:!aNULL:!MD5;
    ssl_session_cache   shared:SSL:10m;

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

    location /keys/ {
        autoindex off;
    }

    access_log /var/log/nginx/repo.access.log;
    error_log  /var/log/nginx/repo.error.log;
}

server {
    listen 80;
    server_name repo.blueberry.linux;
    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }
    location / {
        return 301 https://$host$request_uri;
    }
}
```

### Start it

```sh
cd /srv/blueberry
docker compose up -d
docker compose ps
```

---

## 3. TLS Certificate

```sh
# Get a cert before starting Nginx (standalone mode — nothing on port 80 yet)
docker run --rm -it \
    -v /srv/blueberry/certbot/conf:/etc/letsencrypt \
    -v /srv/blueberry/certbot/webroot:/var/www/certbot \
    -p 80:80 \
    certbot/certbot certonly --standalone \
    -d repo.blueberry.linux \
    --agree-tos --email admin@blueberry.linux

# Auto-renew (add to crontab):
# 0 3 * * * docker run --rm certbot/certbot renew --webroot \
#   -w /srv/blueberry/certbot/webroot && \
#   docker exec blueberry-nginx-1 nginx -s reload
```

---

## 4. Deploy User for rsync

GitHub Actions rsyncs packages to the server over SSH. Create a locked-down
user on the repo server:

```sh
# On the repo server
useradd -r -s /sbin/nologin -d /srv/blueberry/repo deploy
mkdir -p /home/deploy/.ssh && chmod 700 /home/deploy/.ssh

# Paste the public half of REPO_SSH_KEY here:
echo "ssh-ed25519 AAAA... github-actions" > /home/deploy/.ssh/authorized_keys
chmod 600 /home/deploy/.ssh/authorized_keys
chown -R deploy:deploy /home/deploy/.ssh

# Give deploy write access to the repo directory only
chown -R deploy:deploy /srv/blueberry/repo
```

Generate the key pair (on your local machine, not the server):

```sh
ssh-keygen -t ed25519 -C "github-actions-deploy" -f ./deploy_key -N ""
# deploy_key      → add as REPO_SSH_KEY secret in GitHub
# deploy_key.pub  → paste into authorized_keys on the server
```

---

## 5. GitHub Actions Secrets

Go to your repo → **Settings → Secrets and variables → Actions → New repository secret**:

| Secret | Value |
|--------|-------|
| `MINISIGN_PRIVATE_KEY` | Contents of `blueberry-repo.key` (the minisign signing key) |
| `REPO_SSH_KEY` | Contents of `deploy_key` (the SSH private key for rsync) |
| `TAILSCALE_AUTHKEY` | Tailscale auth key — only needed if your repo server has no public IP |

---

## 6. Tailscale (if your repo server is on a private network)

If your repo server is a home NAS (e.g. 192.168.0.79) without a public IP,
GitHub Actions can't reach it directly. Use Tailscale to create a private
tunnel:

```sh
# On the repo server
curl -fsSL https://tailscale.com/install.sh | sh
tailscale up
# Note the Tailscale IP (e.g. 100.x.y.z) — use this in the rsync destination
```

In `.github/workflows/ci.yml`, the `sign-and-publish` job already includes
the Tailscale step. Generate an auth key at tailscale.com/admin/settings/keys
(reusable, ephemeral) and add it as `TAILSCALE_AUTHKEY`.

Update the rsync target in `ci.yml` to use the Tailscale IP:

```yaml
rsync -av --delete /tmp/packages/ \
  deploy@100.x.y.z:/srv/blueberry/repo/packages/x86_64/
```

---

## 7. Package Signing

### Generate a signing key pair

```sh
# On a secure machine (offline ideally)
minisign -G -s blueberry-repo.key -p blueberry-repo.pub \
    -c "Blueberry Linux package repository"
```

- Store `blueberry-repo.key` securely — this is your `MINISIGN_PRIVATE_KEY` secret.
- Copy `blueberry-repo.pub` to `/srv/blueberry/repo/keys/` so clients can fetch it.

### Add the key to a Blueberry install

```sh
mkdir -p /etc/bpm/trusted-keys
wget -O /etc/bpm/trusted-keys/blueberry-repo.pub \
    https://repo.blueberry.linux/keys/blueberry-repo.pub
```

---

## 8. CI Pipeline Summary

`.github/workflows/ci.yml` runs these jobs:

| Job | Trigger | What it does |
|-----|---------|--------------|
| `lint-bpm` | all pushes + PRs | `go fmt` + `go vet` |
| `test-bpm` | all pushes + PRs | `go test -race ./...` |
| `build-bpm` | all pushes + PRs | `make bpm`, uploads binary artifact |
| `build-world` | push to main | `make world JOBS=4`, uploads boot/ artifact |
| `build-packages` | push to main | `make repo`, uploads repo/ artifact |
| `sign-and-publish` | push to main | Signs with minisign, rsyncs to repo server |

---

## 9. Local Package Repository (Air-Gapped / Testing)

To serve packages locally without the CI pipeline:

```sh
make repo
# packages land in ../blueberry-build/repo/

# Serve with Python
python3 -m http.server 8080 --directory ../blueberry-build/repo/

# Or with busybox
busybox httpd -f -p 8080 -h ../blueberry-build/repo/
```

Add to `/etc/bpm/repos.d/local.conf` on target systems:

```toml
name    = "local"
url     = "http://192.168.0.79:8080"
enabled = true
```

---

## 10. Disaster Recovery

### Repo server goes down

Users can install from local `.bb` files: `bpm install --file package.bb`

The entire package set rebuilds from source with `make repo`.

### Signing key compromised

1. Generate a new key pair.
2. Remove the old key from `/etc/bpm/trusted-keys/` on all systems.
3. Rebuild and re-sign all packages.
4. Announce the rotation via a GitHub release / security advisory.
