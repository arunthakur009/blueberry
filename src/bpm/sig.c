/* sig.c — repository index signature verification.
 *
 * The repo signs bpm.index with an ECDSA P-256 private key (SHA-256 digest,
 * DER/asn1 signature — exactly what `openssl dgst -sha256 -sign` produces).
 * bpm verifies that signature against the public key baked into repokey.h.
 *
 * Verification is mandatory unless BPM_ALLOW_UNSIGNED is set in the
 * environment (a dev/testing escape hatch — omitting the signature must not
 * silently downgrade security in production).
 */
#include "bpm.h"
#include "repokey.h"
#include "bearssl_ec.h"
#include <stdlib.h>
#include <string.h>

int sig_required(void) {
    const char *e = getenv("BPM_ALLOW_UNSIGNED");
    return !(e && *e);
}

int sig_verify_index(const void *data, size_t len,
                     const void *sig, size_t siglen) {
    unsigned char hash[32];
    sha256_raw(data, len, hash);

    /* The baked-in point is const; BearSSL's struct wants a non-const q but
     * only reads it. Copy into a local writable buffer to stay correct. */
    unsigned char q[128];
    if (bpm_repo_pubkey_len > sizeof q) return 0;
    memcpy(q, bpm_repo_pubkey, bpm_repo_pubkey_len);

    br_ec_public_key pk = {
        .curve = BR_EC_secp256r1,
        .q = q,
        .qlen = bpm_repo_pubkey_len,
    };

    uint32_t ok = br_ecdsa_i31_vrfy_asn1(&br_ec_p256_m31,
                                         hash, sizeof hash,
                                         &pk, sig, siglen);
    return ok == 1;
}
