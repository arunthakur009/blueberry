/* archive.c — streaming zstd (libzstd) + ustar/pax/GNU tar reader.
 *
 * The reader never holds a whole package in memory: it decompresses in fixed
 * chunks and hands each tar member's payload to a callback through a pull-based
 * ZReader, so even a multi-hundred-MB package (gcc) installs in a few hundred
 * KB of working set. This is what keeps `bpm install gcc` from being OOM-killed
 * on small install VMs.
 *
 * makepkg's bsdtar output is ustar with pax extended headers (type 'x') for
 * long paths and high-resolution mtimes, and occasionally GNU long-name records
 * ('L'/'K'). We honour pax "path"/"linkpath" overrides and GNU long names; all
 * other extended records (mtime, uid, ...) are ignored.
 */
#include "bpm.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <zstd.h>

/* ── streaming decompressor ────────────────────────────────────────────────── */
struct ZReader {
    FILE         *f;
    ZSTD_DStream *ds;
    unsigned char *in;  size_t in_cap;
    ZSTD_inBuffer  ib;
    unsigned char *out; size_t out_cap, out_beg, out_end;
    int            eof;          /* hit EOF on the compressed input */
    size_t         entry_remaining;  /* unread payload bytes of current member */
};

/* Refill the decompressed staging buffer when empty. Returns 1 if bytes are
 * available, 0 at clean end of stream, -1 on a zstd error. */
static int zr_fill(ZReader *zr) {
    if (zr->out_beg < zr->out_end) return 1;
    zr->out_beg = zr->out_end = 0;

    while (zr->out_end == 0) {
        if (zr->ib.pos == zr->ib.size) {
            if (zr->eof) return 0;
            size_t r = fread(zr->in, 1, zr->in_cap, zr->f);
            if (r == 0) { zr->eof = 1; return 0; }
            zr->ib.src = zr->in; zr->ib.size = r; zr->ib.pos = 0;
        }
        ZSTD_outBuffer ob = { zr->out, zr->out_cap, 0 };
        size_t ret = ZSTD_decompressStream(zr->ds, &ob, &zr->ib);
        if (ZSTD_isError(ret)) return -1;
        zr->out_end = ob.pos;
        if (ob.pos == 0 && zr->ib.pos == zr->ib.size) {
            /* consumed all input this round; loop to pull more from the file */
            if (zr->eof) return 0;
        }
    }
    return 1;
}

/* Pull up to n decompressed bytes, ignoring tar member boundaries. dst==NULL
 * skips. Returns bytes actually produced (short only at end of stream). */
static size_t raw_read(ZReader *zr, void *dst, size_t n) {
    unsigned char *d = dst;
    size_t got = 0;
    while (got < n) {
        if (zr->out_beg >= zr->out_end) {
            if (zr_fill(zr) <= 0) break;
        }
        size_t avail = zr->out_end - zr->out_beg;
        size_t take  = n - got; if (take > avail) take = avail;
        if (d) memcpy(d + got, zr->out + zr->out_beg, take);
        zr->out_beg += take;
        got += take;
    }
    return got;
}

/* Public: read up to n bytes of the *current member's* payload (callbacks use
 * this to stream a file straight to disk). Never reads past the member. */
size_t zr_read(ZReader *zr, void *dst, size_t n) {
    if (n > zr->entry_remaining) n = zr->entry_remaining;
    size_t got = raw_read(zr, dst, n);
    zr->entry_remaining -= got;
    return got;
}

/* ── tar helpers ───────────────────────────────────────────────────────────── */
static unsigned parse_octal(const char *p, size_t n) {
    unsigned v = 0;
    while (n && (*p == ' ' || *p == '\0')) { p++; n--; }
    while (n && *p >= '0' && *p <= '7') { v = v * 8 + (unsigned)(*p - '0'); p++; n--; }
    return v;
}

/* Extract "path=" or "linkpath=" value from a pax extended-header block.
 * Records are "<len> <key>=<value>\n". Returns malloc'd value or NULL. */
static char *pax_value(const char *blk, size_t blksz, const char *key) {
    size_t keylen = strlen(key);
    const char *p = blk, *end = blk + blksz;
    while (p < end) {
        const char *sp = memchr(p, ' ', (size_t)(end - p));
        if (!sp) break;
        long reclen = strtol(p, NULL, 10);
        if (reclen <= 0 || p + reclen > end) break;
        const char *kv = sp + 1;                 /* "key=value\n" */
        const char *rec_end = p + reclen;
        const char *eq = memchr(kv, '=', (size_t)(rec_end - kv));
        if (eq && (size_t)(eq - kv) == keylen && !memcmp(kv, key, keylen)) {
            size_t vlen = (size_t)(rec_end - (eq + 1));
            if (vlen && eq[1 + vlen - 1] == '\n') vlen--;   /* drop trailing \n */
            char *v = xmalloc(vlen + 1);
            memcpy(v, eq + 1, vlen); v[vlen] = '\0';
            return v;
        }
        p = rec_end;
    }
    return NULL;
}

/* Read a whole short record (pax/GNU long name) into a malloc'd buffer, then
 * skip its 512-byte padding. Caller frees. Returns NULL on short read. */
static char *read_record(ZReader *zr, size_t size) {
    char *b = xmalloc(size + 1);
    if (raw_read(zr, b, size) != size) { free(b); return NULL; }
    b[size] = '\0';
    size_t pad = (512 - (size % 512)) % 512;
    raw_read(zr, NULL, pad);
    return b;
}

/* ── streaming package iterator ────────────────────────────────────────────── */
int pkg_stream(const char *path, pkg_cb cb, void *ctx) {
    FILE *f = fopen(path, "rb");
    if (!f) return -1;

    ZReader zr;
    memset(&zr, 0, sizeof zr);
    zr.f = f;
    zr.ds = ZSTD_createDStream();
    if (!zr.ds) { fclose(f); return -1; }
    ZSTD_initDStream(zr.ds);
    zr.in_cap  = ZSTD_DStreamInSize();
    zr.out_cap = ZSTD_DStreamOutSize();
    zr.in  = xmalloc(zr.in_cap);
    zr.out = xmalloc(zr.out_cap);

    char hdr[512];
    char *next_path = NULL, *next_link = NULL;
    int empty = 0, rc = 0;

    for (;;) {
        if (raw_read(&zr, hdr, 512) != 512) break;     /* clean end / truncated */

        int zero = 1;
        for (int i = 0; i < 512; i++) if (hdr[i]) { zero = 0; break; }
        if (zero) { if (++empty >= 2) break; continue; }
        empty = 0;

        unsigned size = parse_octal(hdr + 124, 12);
        char type = hdr[156];
        size_t pad = (512 - (size % 512)) % 512;

        if (type == 'x' || type == 'g') {              /* pax extended header */
            char *blk = read_record(&zr, size);
            if (!blk) { rc = -1; break; }
            char *pp = pax_value(blk, size, "path");
            char *lp = pax_value(blk, size, "linkpath");
            if (pp) { free(next_path); next_path = pp; }
            if (lp) { free(next_link); next_link = lp; }
            free(blk);
            continue;
        }
        if (type == 'L') {                             /* GNU long name */
            free(next_path); next_path = read_record(&zr, size);
            if (!next_path) { rc = -1; break; }
            continue;
        }
        if (type == 'K') {                             /* GNU long link */
            free(next_link); next_link = read_record(&zr, size);
            if (!next_link) { rc = -1; break; }
            continue;
        }

        char namebuf[260];   /* 155 prefix + '/' + 100 name + NUL */
        const char *name;
        if (next_path) name = next_path;
        else {
            const char *prefix = hdr + 345;
            if (prefix[0]) snprintf(namebuf, sizeof namebuf, "%.155s/%.100s", prefix, hdr);
            else           snprintf(namebuf, sizeof namebuf, "%.100s", hdr);
            name = namebuf;
        }
        char linkbuf[101];
        const char *link;
        if (next_link) link = next_link;
        else { snprintf(linkbuf, sizeof linkbuf, "%.100s", hdr + 157); link = linkbuf; }

        TarEntry e;
        e.name     = name;
        e.linkname = link;
        e.size     = size;
        e.mode     = parse_octal(hdr + 100, 8) & 07777;
        e.type     = (type == '\0') ? '0' : type;

        zr.entry_remaining = size;
        rc = cb(&e, &zr, ctx);

        /* Drain whatever the callback left, plus padding to the next header. */
        raw_read(&zr, NULL, zr.entry_remaining);
        raw_read(&zr, NULL, pad);

        free(next_path); next_path = NULL;
        free(next_link); next_link = NULL;
        if (rc) break;
    }

    free(next_path); free(next_link);
    ZSTD_freeDStream(zr.ds);
    free(zr.in); free(zr.out); fclose(f);
    return rc;
}
