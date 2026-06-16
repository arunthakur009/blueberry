//! HTTP(S) downloads. TLS is rustls (via ureq); the public repo cert validates
//! against the bundled Mozilla roots, the same trust set as the system
//! ca-certificates bundle. Streams the body to a file — never into RAM.

use std::fs::File;
use std::io;
use std::path::Path;

/// GET `url` into `dest` (truncating). Returns Ok on a 2xx response.
pub fn get(url: &str, dest: &Path) -> io::Result<()> {
    let resp = ureq::get(url)
        .timeout(std::time::Duration::from_secs(60))
        .call()
        .map_err(|e| io::Error::new(io::ErrorKind::Other, e.to_string()))?;
    let mut reader = resp.into_reader();
    let mut out = File::create(dest)?;
    io::copy(&mut reader, &mut out)?;
    Ok(())
}
