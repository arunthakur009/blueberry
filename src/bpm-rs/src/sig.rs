//! Repository index signature verification (ECDSA P-256 / SHA-256, DER).
//!
//! The repo signs `bpm.index` with `openssl dgst -sha256 -sign`; we verify the
//! detached DER signature against the public key baked in below — the same key
//! as the C bpm's `repokey.h`. Mandatory unless `BPM_ALLOW_UNSIGNED` is set.

use crate::repokey::REPO_PUBKEY;
use p256::ecdsa::signature::Verifier;
use p256::ecdsa::{Signature, VerifyingKey};

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
