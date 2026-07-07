## Blueberry Linux — v0.6.2-beta

New packages on the mirror — **rootless containers** and **server ops staples**.
These are install-on-demand, so the base image is unchanged from v0.6.1 (same
ISOs); grab what you need with `bpm install`.

### Rootless containers

`bpm install podman` now pulls a complete **rootless** stack, not just root
containers:

| Package | Role |
|---------|------|
| shadow 4.19.4 | `newuidmap`/`newgidmap` (setuid) + subuid/subgid mapping; real `useradd`/`groupadd` |
| passt 2026_06_11 | `pasta` user-mode network uplink for rootless |
| fuse3 3.18.2 | `fusermount3` (setuid) + libfuse3 |
| fuse-overlayfs 1.17 | rootless overlay storage |

Create a normal user (shadow's `useradd` auto-assigns subordinate uid/gid
ranges) and run containers without root:

```sh
bpm install podman
useradd -m alice && su - alice
podman run --rm docker.io/library/alpine echo "rootless!"
```

### Server ops staples

```sh
bpm install fail2ban       # ban IPs that brute-force sshd (systemd unit incl.)
bpm install msmtp          # sendmail-compatible SMTP for cron/alert mail
bpm install node_exporter  # Prometheus host metrics on :9100
bpm install restic         # fast, deduplicating, encrypted backups
```

### Base image

Unchanged from v0.6.1 (setuid + `/var/tmp` fixes). If you're already on v0.6.1
you don't need to reinstall — just `bpm update` and `bpm install` the above.
