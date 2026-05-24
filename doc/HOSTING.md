# Hosting the Package Repository and Build Server

This guide walks through setting up production infrastructure for Blueberry
Linux development: a Forgejo git instance, a Woodpecker CI build server, and
an Nginx package repository.

All components run as containers via Docker Compose on a single server, or
can be separated across machines.

---

## 1. Architecture Overview

```
Internet
  │
  ├─ repo.blueberry.linux  (Nginx) ── packages & BBINDEX.zst
  ├─ git.blueberry.linux   (Forgejo) ── source code, issues, PRs
  └─ ci.blueberry.linux    (Woodpecker CI) ── builds, triggered by Forgejo

  Build pipeline:
  Developer pushes → Forgejo webhook → Woodpecker → builder container
    → bpm build pkgs/core/* → .bb files → signed → rsync to Nginx
```

---

## 2. Prerequisites

- A Linux server (Debian 12 or Blueberry Linux itself recommended)
- Docker Engine 24+ and Docker Compose v2
- Two or three DNS A records pointing to the server
- A valid TLS certificate (we'll use Certbot + Nginx)
- `minisign` installed on the signing machine (may be separate from the build server)

---

## 3. Directory Layout on the Host

```
/srv/blueberry/
  docker-compose.yml
  nginx/
    conf.d/
      forgejo.conf
      woodpecker.conf
      repo.conf
  forgejo/
    data/          ← Forgejo app data (volumes)
  woodpecker/
    data/
  repo/
    packages/
      x86_64/      ← .bb files + BBINDEX.zst + .minisig files
      aarch64/
    keys/
      blueberry-repo.pub   ← public signing key (served by Nginx)
  certbot/
    conf/
    webroot/
```

---

## 4. Docker Compose File

```yaml
# /srv/blueberry/docker-compose.yml
version: "3.9"

services:

  # ── Nginx reverse proxy + repo server ───────────────────────────────────────
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
    depends_on:
      - forgejo
      - woodpecker

  # ── Forgejo (git + issues + PR) ─────────────────────────────────────────────
  forgejo:
    image: codeberg.org/forgejo/forgejo:7
    restart: unless-stopped
    environment:
      - USER_UID=1000
      - USER_GID=1000
      - FORGEJO__server__DOMAIN=git.blueberry.linux
      - FORGEJO__server__ROOT_URL=https://git.blueberry.linux/
      - FORGEJO__server__SSH_DOMAIN=git.blueberry.linux
      - FORGEJO__database__DB_TYPE=sqlite3
      - FORGEJO__database__PATH=/data/forgejo/gitea.db
      - FORGEJO__security__INSTALL_LOCK=true
      - FORGEJO__service__DISABLE_REGISTRATION=false
    volumes:
      - ./forgejo/data:/data
    ports:
      - "2222:22"    # git SSH access

  # ── Woodpecker CI server ─────────────────────────────────────────────────────
  woodpecker-server:
    image: woodpeckerci/woodpecker-server:v2
    restart: unless-stopped
    environment:
      - WOODPECKER_OPEN=false
      - WOODPECKER_HOST=https://ci.blueberry.linux
      - WOODPECKER_FORGEJO=true
      - WOODPECKER_FORGEJO_URL=http://forgejo:3000
      - WOODPECKER_FORGEJO_CLIENT=<oauth2-client-id>      # see step 6
      - WOODPECKER_FORGEJO_SECRET=<oauth2-client-secret>
      - WOODPECKER_AGENT_SECRET=<random-secret>           # shared with agent
      - WOODPECKER_ADMIN=admin
      - WOODPECKER_DATABASE_DRIVER=sqlite3
      - WOODPECKER_DATABASE_DATASOURCE=/data/woodpecker.db
    volumes:
      - ./woodpecker/data:/data
    depends_on:
      - forgejo

  # ── Woodpecker agent (runs builds) ───────────────────────────────────────────
  woodpecker-agent:
    image: woodpeckerci/woodpecker-agent:v2
    restart: unless-stopped
    environment:
      - WOODPECKER_SERVER=woodpecker-server:9000
      - WOODPECKER_AGENT_SECRET=<same-random-secret>
      - WOODPECKER_BACKEND=docker
      - WOODPECKER_MAX_WORKFLOWS=2
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    depends_on:
      - woodpecker-server
```

---

## 5. Nginx Configuration

### Package Repository (`/srv/blueberry/nginx/conf.d/repo.conf`)

```nginx
# Serve the package repository over HTTPS
server {
    listen 443 ssl http2;
    server_name repo.blueberry.linux;

    ssl_certificate     /etc/letsencrypt/live/repo.blueberry.linux/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/repo.blueberry.linux/privkey.pem;
    ssl_protocols       TLSv1.3 TLSv1.2;
    ssl_ciphers         HIGH:!aNULL:!MD5;
    ssl_session_cache   shared:SSL:10m;

    root /srv/repo;
    autoindex on;     # allow directory listing so bpm can find files

    # BBINDEX must be re-downloadable without caching
    location ~* BBINDEX\.zst$ {
        add_header Cache-Control "no-cache, must-revalidate";
        expires 0;
    }

    # .bb and .minisig files can be cached
    location ~* \.(bb|minisig|pub)$ {
        add_header Cache-Control "public, max-age=86400";
        expires 1d;
    }

    # Don't expose the keys directory listing — only allow direct fetches
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

### Forgejo (`/srv/blueberry/nginx/conf.d/forgejo.conf`)

```nginx
server {
    listen 443 ssl http2;
    server_name git.blueberry.linux;

    ssl_certificate     /etc/letsencrypt/live/git.blueberry.linux/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/git.blueberry.linux/privkey.pem;
    ssl_protocols       TLSv1.3 TLSv1.2;

    client_max_body_size 100M;    # allow large git pushes

    location / {
        proxy_pass http://forgejo:3000;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
    access_log /var/log/nginx/forgejo.access.log;
}
server {
    listen 80;
    server_name git.blueberry.linux;
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 301 https://$host$request_uri; }
}
```

### Woodpecker (`/srv/blueberry/nginx/conf.d/woodpecker.conf`)

```nginx
server {
    listen 443 ssl http2;
    server_name ci.blueberry.linux;

    ssl_certificate     /etc/letsencrypt/live/ci.blueberry.linux/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/ci.blueberry.linux/privkey.pem;
    ssl_protocols       TLSv1.3 TLSv1.2;

    location / {
        proxy_pass         http://woodpecker-server:8000;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto https;
        proxy_http_version 1.1;
        proxy_set_header   Upgrade           $http_upgrade;
        proxy_set_header   Connection        "upgrade";
    }
    access_log /var/log/nginx/woodpecker.access.log;
}
server {
    listen 80;
    server_name ci.blueberry.linux;
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 301 https://$host$request_uri; }
}
```

---

## 6. TLS Certificates

```sh
# Initial certificates (before nginx is running, use standalone mode)
docker run --rm -it \
    -v /srv/blueberry/certbot/conf:/etc/letsencrypt \
    -v /srv/blueberry/certbot/webroot:/var/www/certbot \
    -p 80:80 \
    certbot/certbot certonly --standalone \
    -d repo.blueberry.linux \
    -d git.blueberry.linux \
    -d ci.blueberry.linux \
    --agree-tos --email admin@blueberry.linux

# Renewal (add as a cron job or systemd timer):
# docker run --rm certbot/certbot renew --webroot -w /var/www/certbot
# docker exec nginx nginx -s reload
```

---

## 7. Forgejo First-Run Setup

```sh
cd /srv/blueberry
docker compose up -d forgejo

# Wait for it to start, then visit https://git.blueberry.linux
# Complete the web setup wizard:
#   - Site title: Blueberry Linux
#   - Server domain: git.blueberry.linux
#   - HTTP port: 3000 (internal)
#   - SSH server: enabled, port 22 (mapped to 2222 externally)
#   - Admin account: create the admin user
```

After initial setup, create an OAuth2 application for Woodpecker:

1. Log in as admin → **Settings** → **Applications** → **OAuth2 Applications**
2. Name: `Woodpecker CI`
3. Redirect URI: `https://ci.blueberry.linux/authorize`
4. Copy the **Client ID** and **Client Secret**.
5. Update `docker-compose.yml` with the credentials.
6. `docker compose up -d woodpecker-server woodpecker-agent`

In Woodpecker:
1. Visit `https://ci.blueberry.linux`
2. Log in with your Forgejo credentials.
3. Activate the `blueberry/blueberry` repository.

---

## 8. Woodpecker Pipeline

Create `.woodpecker.yml` in the blueberry git repo:

```yaml
# .woodpecker.yml — Blueberry Linux CI pipeline

when:
  event: [push, pull_request]

steps:
  - name: fetch-sources
    image: alpine:latest
    commands:
      - apk add --no-cache wget tar xz bzip2 zstd
      - make fetch

  - name: build-bpm
    image: golang:1.22-alpine
    commands:
      - apk add --no-cache git
      - make bpm
      - ./obj/bpm --help

  - name: build-world
    image: alpine:latest
    commands:
      - apk add --no-cache build-base musl-dev go wget tar xz bzip2
          zstd cpio perl bc flex bison openssl-dev elfutils-dev
      - make world JOBS=4

  - name: build-packages
    image: alpine:latest
    when:
      branch: main
    commands:
      - make repo
      - ls -lh obj/repo/

  - name: sign-packages
    image: alpine:latest
    when:
      branch: main
      event: push
    secrets: [MINISIGN_PRIVATE_KEY]
    commands:
      - apk add --no-cache minisign
      - echo "$MINISIGN_PRIVATE_KEY" > /tmp/signing.key
      - for bb in obj/repo/*.bb; do
          minisign -S -s /tmp/signing.key -m "$$bb";
        done
      - rm /tmp/signing.key

  - name: publish
    image: alpine:latest
    when:
      branch: main
      event: push
    secrets: [REPO_SSH_KEY]
    commands:
      - apk add --no-cache openssh rsync
      - mkdir -p ~/.ssh && chmod 700 ~/.ssh
      - echo "$REPO_SSH_KEY" > ~/.ssh/id_ed25519 && chmod 600 ~/.ssh/id_ed25519
      - ssh-keyscan repo.blueberry.linux >> ~/.ssh/known_hosts
      - rsync -av --delete obj/repo/
          deploy@repo.blueberry.linux:/srv/blueberry/repo/packages/x86_64/
```

Set secrets in Woodpecker:
- `MINISIGN_PRIVATE_KEY` — the minisign secret key (armored, no passphrase)
- `REPO_SSH_KEY` — SSH private key for rsync to the repo server

---

## 9. Package Signing with minisign

### Generating a signing key pair

```sh
# On a secure offline machine (ideally):
minisign -G -s blueberry-repo.key -p blueberry-repo.pub \
    -c "Blueberry Linux package repository"
```

Store `blueberry-repo.key` securely (password manager, offline vault).
Commit `blueberry-repo.pub` to the repository and serve it at
`https://repo.blueberry.linux/keys/blueberry-repo.pub`.

### Signing packages

```sh
minisign -S -s blueberry-repo.key -m musl-1.2.5-1-x86_64.bb
# Creates musl-1.2.5-1-x86_64.bb.minisig
```

### Verifying a package

```sh
minisign -V -p blueberry-repo.pub -m musl-1.2.5-1-x86_64.bb
```

### bpm verification

bpm checks for a `.minisig` sidecar when `REPO_SIGN=1` is set and a public
key is present in `/etc/bpm/trusted-keys/`. To add the key:

```sh
mkdir -p /etc/bpm/trusted-keys
wget -O /etc/bpm/trusted-keys/blueberry-repo.pub \
    https://repo.blueberry.linux/keys/blueberry-repo.pub
```

---

## 10. Local Package Repository (Air-Gapped)

To serve packages from a local directory (no internet):

```sh
# Build packages
make repo
# obj/repo/ now contains .bb files and BBINDEX.zst

# Serve locally with busybox httpd
busybox httpd -f -p 8080 -h obj/repo/

# Or with Python
python3 -m http.server 8080 --directory obj/repo/
```

Add to `/etc/bpm/repos.d/local.conf` on target systems:

```toml
name    = "local"
url     = "http://192.168.1.100:8080"
enabled = true
```

---

## 11. Build Container (Reproducible Builds)

For reproducible package builds, use a Docker container based on Alpine:

```dockerfile
# tools/Dockerfile.builder
FROM alpine:3.20

RUN apk add --no-cache \
    build-base musl-dev go \
    wget curl tar xz bzip2 zstd \
    cpio perl bc flex bison \
    openssl-dev elfutils-dev \
    xorriso squashfs-tools \
    minisign

RUN go install golang.org/x/tools/cmd/goimports@latest 2>/dev/null || true

WORKDIR /build
ENTRYPOINT ["make"]
```

Build and use:
```sh
docker build -f tools/Dockerfile.builder -t blueberry-builder .
docker run --rm -v $(pwd):/build blueberry-builder world
```

---

## 12. Repository File Layout

```
/srv/blueberry/repo/
  packages/
    x86_64/
      BBINDEX.zst              ← package index
      musl-1.2.5-1-x86_64.bb
      musl-1.2.5-1-x86_64.bb.minisig
      busybox-1.36.1-2-x86_64.bb
      busybox-1.36.1-2-x86_64.bb.minisig
      openssh-9.8p1-1-x86_64.bb
      openssh-9.8p1-1-x86_64.bb.minisig
      ...
    aarch64/
      BBINDEX.zst
      musl-1.2.5-1-aarch64.bb
      ...
  keys/
    blueberry-repo.pub
```

---

## 13. Monitoring the Repository

Track package download statistics in Nginx access logs:

```sh
# Packages downloaded in the last hour
grep ".bb" /var/log/nginx/repo.access.log | \
    awk '{print $7}' | sort | uniq -c | sort -rn | head -20

# Total bandwidth used today
awk '{sum += $10} END {printf "%.1f GB\n", sum/1024/1024/1024}' \
    /var/log/nginx/repo.access.log
```

For proper metrics, use `nginx-prometheus-exporter` with Grafana.

---

## 14. Disaster Recovery

### If the repo server goes down

1. Mirror packages are served from any other Nginx instance.
2. Users can install from local `.bb` files with `bpm install --file`.
3. The entire package set can be rebuilt from the source tree with `make repo`.

### If the signing key is compromised

1. Generate a new key pair.
2. Remove the old key from `/etc/bpm/trusted-keys/` on all systems.
3. Rebuild and re-sign all packages.
4. Announce the key rotation via the git repo and security advisory.

### Database backup (Forgejo + Woodpecker)

```sh
# Daily backup of Forgejo data
tar -czf /backups/forgejo-$(date +%Y%m%d).tar.gz /srv/blueberry/forgejo/data/

# Woodpecker is stateless for packages; only the build history DB matters
sqlite3 /srv/blueberry/woodpecker/data/woodpecker.db ".backup /backups/wp-$(date +%Y%m%d).db"
```
