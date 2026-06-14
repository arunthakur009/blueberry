/* net.c — HTTP/1.1 GET over a pluggable transport (plain TCP or TLS).
 * Handles Content-Length, chunked transfer-encoding, connection-close bodies,
 * and a few redirects. http:// uses raw sockets; https:// uses tls.c (BearSSL). */
#define _GNU_SOURCE
#include "bpm.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>
#include <sys/socket.h>
#include <netdb.h>
#include <poll.h>
#include <fcntl.h>

/* A transport: read returns bytes (0=EOF, <0 err); write returns 0 ok/-1. */
typedef struct {
    void *ctx;
    int (*read)(void *ctx, void *buf, size_t n);
    int (*write)(void *ctx, const void *buf, size_t n);
    void (*close)(void *ctx);
} Conn;

/* plain-socket transport (ctx = &fd) */
static int sk_read(void *ctx, void *buf, size_t n) {
    int fd = *(int *)ctx;
    for (;;) {
        ssize_t r = read(fd, buf, n);
        if (r < 0) { if (errno == EINTR) continue; return -1; }
        return (int)r;            /* 0 == EOF */
    }
}
static int sk_write(void *ctx, const void *buf, size_t n) {
    int fd = *(int *)ctx;
    const char *p = buf; size_t put = 0;
    while (put < n) {
        ssize_t w = write(fd, p + put, n - put);
        if (w < 0) { if (errno == EINTR) continue; return -1; }
        put += (size_t)w;
    }
    return 0;
}

/* Parse http(s)://host[:port]/path. Sets *https. 0 ok, -1 unsupported scheme. */
static int parse_url(const char *url, int *https, char **host, char **port, char **path) {
    const char *p;
    if (!strncmp(url, "https://", 8)) { *https = 1; p = url + 8; }
    else if (!strncmp(url, "http://", 7)) { *https = 0; p = url + 7; }
    else return -1;

    const char *slash = strchr(p, '/');
    const char *hostend = slash ? slash : p + strlen(p);
    const char *colon = memchr(p, ':', (size_t)(hostend - p));
    size_t hlen = colon ? (size_t)(colon - p) : (size_t)(hostend - p);
    *host = xmalloc(hlen + 1);
    memcpy(*host, p, hlen); (*host)[hlen] = '\0';

    if (colon) {
        size_t plen = (size_t)(hostend - (colon + 1));
        *port = xmalloc(plen + 1);
        memcpy(*port, colon + 1, plen); (*port)[plen] = '\0';
    } else {
        *port = xstrdup(*https ? "443" : "80");
    }
    *path = xstrdup(slash ? slash : "/");
    return 0;
}

/* Connect to one resolved address with a timeout (non-blocking + poll), then
 * restore blocking mode. -1 on failure/timeout. */
static int connect_one(struct addrinfo *rp, int timeout_ms) {
    int fd = socket(rp->ai_family, rp->ai_socktype | SOCK_NONBLOCK, rp->ai_protocol);
    if (fd < 0) return -1;
    int r = connect(fd, rp->ai_addr, rp->ai_addrlen);
    if (r != 0) {
        if (errno != EINPROGRESS) { close(fd); return -1; }
        struct pollfd pfd = { fd, POLLOUT, 0 };
        if (poll(&pfd, 1, timeout_ms) <= 0) { close(fd); return -1; }
        int err = 0; socklen_t el = sizeof err;
        if (getsockopt(fd, SOL_SOCKET, SO_ERROR, &err, &el) != 0 || err != 0) {
            close(fd); return -1;
        }
    }
    int fl = fcntl(fd, F_GETFL, 0);
    if (fl != -1) fcntl(fd, F_SETFL, fl & ~O_NONBLOCK);
    return fd;
}

int tcp_connect(const char *host, const char *port) {
    struct addrinfo hints, *res, *rp;
    memset(&hints, 0, sizeof hints);
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;
    if (getaddrinfo(host, port, &hints, &res) != 0) return -1;
    int fd = -1;
    /* IPv4 first, then IPv6: on dual-stack hosts with broken v6 routing this
     * avoids a long stall trying an unreachable AAAA before the working A. */
    int order[2] = { AF_INET, AF_INET6 };
    for (int pass = 0; pass < 2 && fd < 0; pass++)
        for (rp = res; rp; rp = rp->ai_next) {
            if (rp->ai_family != order[pass]) continue;
            fd = connect_one(rp, 8000);
            if (fd >= 0) break;
        }
    freeaddrinfo(res);
    return fd;
}

/* Read into b until "\r\n\r\n"; leftover body bytes stay in b after *hdr_end. */
static int read_headers(Conn *c, Buf *b, size_t *hdr_end) {
    for (;;) {
        char tmp[2048];
        int r = c->read(c->ctx, tmp, sizeof tmp);
        if (r < 0) return -1;
        if (r == 0) return -1;
        buf_append(b, tmp, (size_t)r);
        /* search for header terminator */
        if (b->len >= 4) {
            for (size_t i = (b->len > (size_t)r + 3 ? b->len - r - 3 : 0);
                 i + 4 <= b->len; i++) {
                if (memcmp(b->data + i, "\r\n\r\n", 4) == 0) {
                    *hdr_end = i + 4; return 0;
                }
            }
        }
        if (b->len > 1 << 18) return -1;     /* runaway headers */
    }
}

static const char *hdr_find(const char *buf, size_t end, const char *name) {
    size_t nlen = strlen(name);
    for (size_t i = 0; i + nlen < end; i++) {
        if ((i == 0 || buf[i-1] == '\n') && strncasecmp(buf + i, name, nlen) == 0
            && buf[i + nlen] == ':') {
            const char *v = buf + i + nlen + 1;
            while (*v == ' ' || *v == '\t') v++;
            return v;
        }
    }
    return NULL;
}

/* Perform one request/response over an established Conn. Writes body to
 * outpath on 2xx. Returns HTTP status (or -1). On 3xx, *location is set
 * (malloc'd) if present. */
static int http_exchange(Conn *c, const char *host, const char *path,
                         const char *outpath, char **location) {
    *location = NULL;
    char *req = xasprintf(
        "GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: bpm/%s\r\n"
        "Accept: */*\r\nConnection: close\r\n\r\n", path, host, BPM_VERSION);
    int wr = c->write(c->ctx, req, strlen(req));
    free(req);
    if (wr < 0) return -1;

    Buf hb; buf_init(&hb);
    size_t hdr_end = 0;
    if (read_headers(c, &hb, &hdr_end) != 0) { buf_free(&hb); return -1; }

    int status = (hb.len > 12) ? atoi(hb.data + 9) : -1;

    if (status >= 300 && status < 400) {
        const char *loc = hdr_find(hb.data, hdr_end, "Location");
        if (loc) {
            const char *e = strpbrk(loc, "\r\n");
            size_t llen = e ? (size_t)(e - loc) : strlen(loc);
            char *l = xmalloc(llen + 1); memcpy(l, loc, llen); l[llen] = '\0';
            *location = l;
        }
        buf_free(&hb); return status;
    }
    if (status < 200 || status >= 300) { buf_free(&hb); return status; }

    int chunked = 0;
    const char *te = hdr_find(hb.data, hdr_end, "Transfer-Encoding");
    if (te && strncasecmp(te, "chunked", 7) == 0) chunked = 1;
    long clen = -1;
    const char *cl = hdr_find(hb.data, hdr_end, "Content-Length");
    if (cl) clen = atol(cl);

    FILE *out = fopen(outpath, "wb");
    if (!out) { buf_free(&hb); return -1; }

    Buf body; buf_init(&body);
    if (hb.len > hdr_end) buf_append(&body, hb.data + hdr_end, hb.len - hdr_end);
    buf_free(&hb);

    int ok = 1;
    if (chunked) {
        size_t pos = 0;
        for (;;) {
            char *nl;
            while (!(nl = memchr(body.data + pos, '\n', body.len - pos))) {
                char tmp[65536];
                int r = c->read(c->ctx, tmp, sizeof tmp);
                if (r <= 0) { ok = 0; break; }
                buf_append(&body, tmp, (size_t)r);
            }
            if (!ok) break;
            long csz = strtol(body.data + pos, NULL, 16);
            pos = (size_t)(nl - body.data) + 1;
            if (csz == 0) break;
            while (body.len - pos < (size_t)csz + 2) {
                char tmp[65536];
                int r = c->read(c->ctx, tmp, sizeof tmp);
                if (r <= 0) { ok = 0; break; }
                buf_append(&body, tmp, (size_t)r);
            }
            if (!ok) break;
            fwrite(body.data + pos, 1, (size_t)csz, out);
            pos += (size_t)csz + 2;
        }
    } else {
        if (body.len) fwrite(body.data, 1, body.len, out);
        long remaining = (clen >= 0) ? clen - (long)body.len : -1;
        char tmp[65536];
        while (remaining != 0) {
            int r = c->read(c->ctx, tmp, sizeof tmp);
            if (r < 0) { ok = 0; break; }
            if (r == 0) break;
            if (remaining > 0 && (long)r > remaining) r = (int)remaining;
            fwrite(tmp, 1, (size_t)r, out);
            if (remaining > 0) remaining -= r;
        }
    }
    buf_free(&body);
    fclose(out);
    return ok ? status : -1;
}

#define MAX_REDIRECTS 5

int http_get(const char *url, const char *outpath) {
    char *cur = xstrdup(url);
    int rc = -1;

    for (int hop = 0; hop < MAX_REDIRECTS; hop++) {
        int https; char *host = NULL, *port = NULL, *path = NULL;
        if (parse_url(cur, &https, &host, &port, &path) != 0) break;

        Conn c; int fd = -1; void *tls = NULL;
        if (https) {
            tls = tls_open(host, port);
            if (!tls) { free(host); free(port); free(path); break; }
            c.ctx = tls; c.read = tls_read; c.write = tls_write; c.close = NULL;
        } else {
            fd = tcp_connect(host, port);
            if (fd < 0) { free(host); free(port); free(path); break; }
            c.ctx = &fd; c.read = sk_read; c.write = sk_write; c.close = NULL;
        }

        char *location = NULL;
        int status = http_exchange(&c, host, path, outpath, &location);

        if (tls) tls_close(tls);
        if (fd >= 0) close(fd);
        free(host); free(port); free(path);

        if (status >= 200 && status < 300) { free(location); rc = 0; break; }
        if (status >= 300 && status < 400 && location) {
            free(cur); cur = location;
            continue;               /* follow redirect (may switch scheme) */
        }
        free(location);
        break;
    }
    free(cur);
    return rc;
}
