//! HTTP(S) downloads. TLS is rustls (via ureq); the public repo cert validates
//! against the bundled Mozilla roots, the same trust set as the system
//! ca-certificates bundle. Streams the body to a file — never into RAM.

use std::fs::{File, OpenOptions};
use std::io::{self, IsTerminal, Read, Write};
use std::net::{SocketAddr, ToSocketAddrs};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU64, AtomicUsize, Ordering};
use std::sync::OnceLock;
use std::time::{Duration, Instant};

/// Shared, thread-safe download progress across parallel fetches. Workers report
/// bytes as they stream; a monitor thread renders one aggregate line (pacman
/// style: packages done/total, MiB done/total, percent, and speed).
pub struct Progress {
    total_pkgs: usize,
    done_pkgs: AtomicUsize,
    total_bytes: AtomicU64, // sum of Content-Lengths as each download starts
    done_bytes: AtomicU64,
    start: Instant,
    stop: AtomicBool,
}

impl Progress {
    pub fn new(total_pkgs: usize) -> Self {
        Progress {
            total_pkgs,
            done_pkgs: AtomicUsize::new(0),
            total_bytes: AtomicU64::new(0),
            done_bytes: AtomicU64::new(0),
            start: Instant::now(),
            stop: AtomicBool::new(false),
        }
    }
    fn add_total(&self, n: u64) { self.total_bytes.fetch_add(n, Ordering::Relaxed); }
    fn add(&self, n: u64) { self.done_bytes.fetch_add(n, Ordering::Relaxed); }
    pub fn finish_pkg(&self) { self.done_pkgs.fetch_add(1, Ordering::Relaxed); }
    pub fn stop(&self) { self.stop.store(true, Ordering::Relaxed); }

    fn line(&self) -> String {
        let done = self.done_bytes.load(Ordering::Relaxed);
        let total = self.total_bytes.load(Ordering::Relaxed);
        let dp = self.done_pkgs.load(Ordering::Relaxed);
        let secs = self.start.elapsed().as_secs_f64().max(0.001);
        let speed = done as f64 / secs / 1_048_576.0; // MiB/s
        let mib = |b: u64| b as f64 / 1_048_576.0;
        let pct = if total > 0 {
            (done as f64 / total as f64 * 100.0).min(100.0)
        } else {
            0.0
        };
        format!(
            ":: downloading  {}/{} pkgs  {:.1}/{:.1} MiB  {:>3.0}%  {:.1} MiB/s",
            dp, self.total_pkgs, mib(done), mib(total), pct, speed
        )
    }

    /// Run in its own thread: repaint the aggregate line until `stop()`, then
    /// clear it (the caller prints the final summary).
    pub fn run(&self) {
        let tty = std::io::stderr().is_terminal();
        while !self.stop.load(Ordering::Relaxed) {
            if tty {
                eprint!("\r\x1b[K{}", self.line());
                let _ = std::io::stderr().flush();
            }
            std::thread::sleep(Duration::from_millis(150));
        }
        if tty {
            eprint!("\r\x1b[K");
            let _ = std::io::stderr().flush();
        }
    }
}


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

fn other(e: impl std::fmt::Display) -> io::Error {
    io::Error::new(io::ErrorKind::Other, e.to_string())
}

/// GET `url` into `dest`. Downloads to `<dest>.part` and renames on success, so
/// `dest` only ever exists complete. If a `.part` from an interrupted run is
/// present, resume it with a Range request (the server may ignore it, in which
/// case we restart). Returns Ok on success.
pub fn get(url: &str, dest: &Path, prog: Option<&Progress>) -> io::Result<()> {
    let part = PathBuf::from(format!("{}.part", dest.display()));
    let have = std::fs::metadata(&part).map(|m| m.len()).unwrap_or(0);

    let (resp, append) = if have > 0 {
        let r = agent()
            .get(url)
            .set("Range", &format!("bytes={have}-"))
            .call()
            .map_err(other)?;
        let resumed = r.status() == 206; // 206 = partial; 200 = full, restart
        (r, resumed)
    } else {
        (agent().get(url).call().map_err(other)?, false)
    };

    // Register this download's size (Content-Length + any already-resumed bytes)
    // so the aggregate percentage/total is accurate.
    if let Some(p) = prog {
        let clen = resp
            .header("Content-Length")
            .and_then(|v| v.parse::<u64>().ok())
            .unwrap_or(0);
        let already = if append { have } else { 0 };
        p.add_total(clen + already);
        if already > 0 {
            p.add(already);
        }
    }

    let mut reader = resp.into_reader();
    let mut out = if append {
        OpenOptions::new().append(true).open(&part)?
    } else {
        File::create(&part)?
    };
    // Stream in chunks, reporting bytes to the progress meter (io::copy hides them).
    let mut buf = vec![0u8; 65536];
    loop {
        let n = reader.read(&mut buf)?;
        if n == 0 {
            break;
        }
        out.write_all(&buf[..n])?;
        if let Some(p) = prog {
            p.add(n as u64);
        }
    }
    out.sync_all()?;
    drop(out);
    std::fs::rename(&part, dest)?;
    Ok(())
}
