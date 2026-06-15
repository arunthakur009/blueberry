/* vercmp.c — package version comparison (alpm/rpmvercmp algorithm).
 *
 * Compares versions of the form [epoch:]pkgver[-pkgrel], e.g. "16.1.0-2".
 * Returns <0 if a<b, 0 if equal, >0 if a>b. This is what decides whether a
 * repo version is genuinely newer than what's installed (a plain strcmp would
 * call 1.10 < 1.9 and "upgrade" sideways/backwards).
 *
 * Algorithm: split each version into alternating runs of digits and letters,
 * separated by any non-alphanumeric. Compare run by run — numeric runs as
 * integers (leading zeros stripped), alpha runs lexically; a numeric run
 * outranks an alpha run; a longer numeric tail outranks a shorter one.
 */
#include "bpm.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <ctype.h>

/* Compare one pkgver/pkgrel/epoch segment (no separators stripped specially). */
static int rpmvercmp(const char *a, const char *b) {
    if (!strcmp(a, b)) return 0;

    char *one = xstrdup(a), *two = xstrdup(b);
    char *p1 = one, *p2 = two;
    int ret = 0;

    while (*p1 || *p2) {
        while (*p1 && !isalnum((unsigned char)*p1)) p1++;
        while (*p2 && !isalnum((unsigned char)*p2)) p2++;

        /* If we ran out of one side, the one with more segments is newer. */
        if (!*p1 || !*p2) break;

        char *s1 = p1, *s2 = p2;
        int isnum;
        if (isdigit((unsigned char)*p1)) {
            while (isdigit((unsigned char)*p1)) p1++;
            while (isdigit((unsigned char)*p2)) p2++;
            isnum = 1;
        } else {
            while (isalpha((unsigned char)*p1)) p1++;
            while (isalpha((unsigned char)*p2)) p2++;
            isnum = 0;
        }

        char sep1 = *p1, sep2 = *p2;
        *p1 = '\0'; *p2 = '\0';

        /* One side numeric, the other alpha (or empty): numeric wins. */
        if (s1 == p1) { ret = isnum ? 1 : -1; break; }   /* p2 had the run */
        if (s2 == p2) { ret = isnum ? 1 : -1; break; }

        if (isnum) {
            while (*s1 == '0') s1++;
            while (*s2 == '0') s2++;
            size_t l1 = strlen(s1), l2 = strlen(s2);
            if (l1 != l2) { ret = l1 < l2 ? -1 : 1; *p1 = sep1; *p2 = sep2; break; }
        }
        int c = strcmp(s1, s2);
        if (c) { ret = c < 0 ? -1 : 1; *p1 = sep1; *p2 = sep2; break; }

        *p1 = sep1; *p2 = sep2;
    }

    if (ret == 0) {
        /* equal so far; whichever still has alphanumeric tail is newer */
        while (*p1 && !isalnum((unsigned char)*p1)) p1++;
        while (*p2 && !isalnum((unsigned char)*p2)) p2++;
        if (*p1 && !*p2) ret = 1;
        else if (!*p1 && *p2) ret = -1;
    }

    free(one); free(two);
    return ret;
}

/* Split "[epoch:]ver[-rel]" into epoch/ver/rel (into caller buffers). */
static void split_evr(const char *v, char *ep, char *ver, char *rel, size_t n) {
    ep[0] = '0'; ep[1] = '\0';
    const char *colon = strchr(v, ':');
    const char *start = v;
    if (colon) {
        size_t l = (size_t)(colon - v); if (l >= n) l = n - 1;
        memcpy(ep, v, l); ep[l] = '\0';
        start = colon + 1;
    }
    const char *dash = strrchr(start, '-');
    if (dash) {
        size_t l = (size_t)(dash - start); if (l >= n) l = n - 1;
        memcpy(ver, start, l); ver[l] = '\0';
        snprintf(rel, n, "%s", dash + 1);
    } else {
        snprintf(ver, n, "%s", start);
        rel[0] = '\0';
    }
}

int vercmp(const char *a, const char *b) {
    char ea[64], va[256], ra[64], eb[64], vb[256], rb[64];
    split_evr(a, ea, va, ra, sizeof va);
    split_evr(b, eb, vb, rb, sizeof vb);

    int c = rpmvercmp(ea, eb);          /* epoch dominates */
    if (c) return c;
    c = rpmvercmp(va, vb);              /* then pkgver */
    if (c) return c;
    if (*ra && *rb) return rpmvercmp(ra, rb);  /* then pkgrel, if both present */
    return 0;
}
