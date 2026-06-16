//! HTTP(S) downloads. TLS is rustls (via ureq); the public repo cert validates
//! against the bundled Mozilla roots, the same trust set as the system
//! ca-certificates bundle. Streams the body to a file — never into RAM.

use std::fs::File;
use std::io;
use std::net::{SocketAddr, ToSocketAddrs};
use std::path::Path;
use std::sync::OnceLock;
use std::time::Duration;

/// Shared agent with an IPv4-first resolver. Many setups (notably QEMU SLIRP)
/// hand out a site-local IPv6 with no route to the internet; a dual-stack repo
/// then resolves to an unreachable AAAA and downloads hang/fail. Trying IPv4
/// first makes bpm work regardless of a broken/half-configured IPv6 stack.
fn agent() -> &'static ureq::Agent {
    static AGENT: OnceLock<ureq::Agent> = OnceLock::new();
    AGENT.get_or_init(|| {
        ureq::builder()
            // Connect timeout fails fast on an unreachable mirror; the read
            // timeout is per-read (resets while bytes flow), so a large package
            // (gcc is ~84 MB) downloads for as long as it keeps making progress.
            // A single overall timeout would kill big downloads on slow links.
            .timeout_connect(Duration::from_secs(20))
            .timeout_read(Duration::from_secs(120))
            .resolver(|netloc: &str| -> io::Result<Vec<SocketAddr>> {
                let mut addrs: Vec<SocketAddr> = netloc.to_socket_addrs()?.collect();
                addrs.sort_by_key(SocketAddr::is_ipv6); // IPv4 (false) before IPv6
                Ok(addrs)
            })
            .build()
    })
}

/// GET `url` into `dest` (truncating). Returns Ok on a 2xx response.
pub fn get(url: &str, dest: &Path) -> io::Result<()> {
    let resp = agent()
        .get(url)
        .call()
        .map_err(|e| io::Error::new(io::ErrorKind::Other, e.to_string()))?;
    let mut reader = resp.into_reader();
    let mut out = File::create(dest)?;
    io::copy(&mut reader, &mut out)?;
    Ok(())
}
