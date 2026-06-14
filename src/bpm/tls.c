/* tls.c — HTTPS transport for bpm via BearSSL.
 *
 * Loads X.509 trust anchors from the system CA bundle (PEM) at runtime,
 * connects, performs a TLS handshake with SNI + full certificate validation,
 * and exposes read/write callbacks the HTTP layer (net.c) drives. No OpenSSL.
 */
#define _GNU_SOURCE
#include "bpm.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>
#include "bearssl/inc/bearssl.h"

/* ── trust anchors (loaded once, cached) ───────────────────────────────────── */
static br_x509_trust_anchor *g_tas;
static size_t g_ntas;
static int    g_tas_loaded;

static void *blobdup(const void *src, size_t len) {
    void *p = xmalloc(len);
    memcpy(p, src, len);
    return p;
}
static void dn_append(void *ctx, const void *buf, size_t len) {
    buf_append((Buf *)ctx, buf, len);
}

/* Build a trust anchor from one DER certificate (adapted from BearSSL's
 * tools/certs.c). Returns 0 on success, -1 to skip. */
static int cert_to_ta(br_x509_trust_anchor *ta, const unsigned char *der, size_t derlen) {
    br_x509_decoder_context dc;
    Buf vdn; buf_init(&vdn);
    br_x509_decoder_init(&dc, dn_append, &vdn);
    br_x509_decoder_push(&dc, der, derlen);
    br_x509_pkey *pk = br_x509_decoder_get_pkey(&dc);
    if (!pk) { buf_free(&vdn); return -1; }

    ta->dn.data = (unsigned char *)vdn.data;   /* transfer ownership */
    ta->dn.len  = vdn.len;
    ta->flags   = br_x509_decoder_isCA(&dc) ? BR_X509_TA_CA : 0;

    if (pk->key_type == BR_KEYTYPE_RSA) {
        ta->pkey.key_type = BR_KEYTYPE_RSA;
        ta->pkey.key.rsa.n    = blobdup(pk->key.rsa.n, pk->key.rsa.nlen);
        ta->pkey.key.rsa.nlen = pk->key.rsa.nlen;
        ta->pkey.key.rsa.e    = blobdup(pk->key.rsa.e, pk->key.rsa.elen);
        ta->pkey.key.rsa.elen = pk->key.rsa.elen;
    } else if (pk->key_type == BR_KEYTYPE_EC) {
        ta->pkey.key_type = BR_KEYTYPE_EC;
        ta->pkey.key.ec.curve = pk->key.ec.curve;
        ta->pkey.key.ec.q     = blobdup(pk->key.ec.q, pk->key.ec.qlen);
        ta->pkey.key.ec.qlen  = pk->key.ec.qlen;
    } else {
        free(vdn.data);
        return -1;
    }
    return 0;
}

/* Parse a PEM CA bundle into trust anchors (BearSSL PEM decode loop). */
static void load_trust_anchors(void) {
    if (g_tas_loaded) return;
    g_tas_loaded = 1;

    size_t len; char *pem = read_file(g_cafile, &len);
    if (!pem) { warn("no CA bundle at %s — HTTPS will fail", g_cafile); return; }

    br_pem_decoder_context pc;
    br_pem_decoder_init(&pc);
    Buf der; buf_init(&der);
    int in_cert = 0, extra_nl = 1;
    const unsigned char *buf = (const unsigned char *)pem;
    size_t rem = len;

    while (rem > 0) {
        size_t pushed = br_pem_decoder_push(&pc, buf, rem);
        buf += pushed; rem -= pushed;
        switch (br_pem_decoder_event(&pc)) {
        case BR_PEM_BEGIN_OBJ:
            in_cert = strstr(br_pem_decoder_name(&pc), "CERTIFICATE") != NULL;
            buf_free(&der); buf_init(&der);
            if (in_cert) br_pem_decoder_setdest(&pc, dn_append, &der);
            else         br_pem_decoder_setdest(&pc, NULL, NULL);
            break;
        case BR_PEM_END_OBJ:
            if (in_cert && der.len) {
                br_x509_trust_anchor ta;
                if (cert_to_ta(&ta, (unsigned char *)der.data, der.len) == 0) {
                    g_tas = xrealloc(g_tas, (g_ntas + 1) * sizeof *g_tas);
                    g_tas[g_ntas++] = ta;
                }
            }
            buf_free(&der); buf_init(&der); in_cert = 0;
            break;
        case BR_PEM_ERROR:
            in_cert = 0;
            break;
        }
        if (rem == 0 && extra_nl) {        /* tolerate missing final newline */
            extra_nl = 0;
            buf = (const unsigned char *)"\n";
            rem = 1;
        }
    }
    buf_free(&der);
    free(pem);
}

/* ── connection ────────────────────────────────────────────────────────────── */
struct TlsConn {
    int fd;
    br_ssl_client_context sc;
    br_x509_minimal_context xc;
    br_sslio_context ioc;
    unsigned char iobuf[BR_SSL_BUFSIZE_BIDI];
};

static int sock_read(void *ctx, unsigned char *buf, size_t len) {
    int fd = *(int *)ctx;
    for (;;) {
        ssize_t r = read(fd, buf, len);
        if (r < 0) { if (errno == EINTR) continue; return -1; }
        if (r == 0) return -1;
        return (int)r;
    }
}
static int sock_write(void *ctx, const unsigned char *buf, size_t len) {
    int fd = *(int *)ctx;
    for (;;) {
        ssize_t w = write(fd, buf, len);
        if (w < 0) { if (errno == EINTR) continue; return -1; }
        return (int)w;
    }
}

void *tls_open(const char *host, const char *port) {
    load_trust_anchors();
    if (g_ntas == 0) { warn("no trust anchors loaded; cannot verify TLS"); return NULL; }

    struct TlsConn *t = xmalloc(sizeof *t);
    t->fd = tcp_connect(host, port);
    if (t->fd < 0) { free(t); return NULL; }

    br_ssl_client_init_full(&t->sc, &t->xc, g_tas, g_ntas);
    br_ssl_engine_set_buffer(&t->sc.eng, t->iobuf, sizeof t->iobuf, 1);
    br_ssl_client_reset(&t->sc, host, 0);
    br_sslio_init(&t->ioc, &t->sc.eng, sock_read, &t->fd, sock_write, &t->fd);
    return t;
}

int tls_read(void *ctx, void *buf, size_t n) {
    struct TlsConn *t = ctx;
    int r = br_sslio_read(&t->ioc, buf, n);
    if (r < 0) {
        /* clean close vs error */
        return (br_ssl_engine_last_error(&t->sc.eng) == 0) ? 0 : -1;
    }
    return r;
}

int tls_write(void *ctx, const void *buf, size_t n) {
    struct TlsConn *t = ctx;
    if (br_sslio_write_all(&t->ioc, buf, n) < 0) return -1;
    if (br_sslio_flush(&t->ioc) < 0) return -1;
    return 0;
}

void tls_close(void *ctx) {
    struct TlsConn *t = ctx;
    if (!t) return;
    br_sslio_close(&t->ioc);
    if (t->fd >= 0) close(t->fd);
    free(t);
}
