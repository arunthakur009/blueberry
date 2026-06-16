//! Repository index signature verification (ECDSA P-256 / SHA-256, DER).
//!
//! The repo signs `bpm.index` with `openssl dgst -sha256 -sign`; we verify the
//! detached DER signature against the public key baked in below — the same key
//! as the C bpm's `repokey.h`. Mandatory unless `BPM_ALLOW_UNSIGNED` is set.

use p256::ecdsa::signature::Verifier;
use p256::ecdsa::{Signature, VerifyingKey};

/// Uncompressed EC point 0x04 || X(32) || Y(32). Mirrors src/bpm/repokey.h.
const REPO_PUBKEY: [u8; 65] = [
    0x04, 0x87, 0xf9, 0x7e, 0x1e, 0xb9, 0xb2, 0x37, 0x18, 0xbe, 0x16, 0xf1, 0x03, 0x20, 0x9c, 0xf2,
    0xa6, 0x05, 0x65, 0xd3, 0x61, 0xc4, 0x20, 0x35, 0xb6, 0x3e, 0xeb, 0x2e, 0xf3, 0x25, 0x8e, 0xc5,
    0x01, 0xcc, 0x88, 0x5d, 0xfe, 0xd6, 0x0f, 0x44, 0x97, 0x9c, 0x7a, 0x03, 0xf4, 0x07, 0xa7, 0x65,
    0xc4, 0x7a, 0x3f, 0x27, 0xae, 0xa8, 0x92, 0xd1, 0x6b, 0x24, 0xfa, 0x7a, 0x03, 0xd4, 0xb8, 0xf5,
    0xd2,
];

/// Verification required unless the dev escape hatch is set.
pub fn required() -> bool {
    std::env::var_os("BPM_ALLOW_UNSIGNED").is_none()
}

/// True if `sig` (DER) is a valid signature over `data` for the baked key.
pub fn verify_index(data: &[u8], sig: &[u8]) -> bool {
    let vk = match VerifyingKey::from_sec1_bytes(&REPO_PUBKEY) {
        Ok(k) => k,
        Err(_) => return false,
    };
    let sig = match Signature::from_der(sig) {
        Ok(s) => s,
        Err(_) => return false,
    };
    // p256's Verifier hashes the message with SHA-256 (NistP256's digest), so
    // this matches `openssl dgst -sha256 -sign` exactly.
    vk.verify(data, &sig).is_ok()
}
