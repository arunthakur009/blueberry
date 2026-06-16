//! Install-root layout, matching the C bpm exactly so the two are
//! interchangeable on a live system (same DB, cache and index on disk).

use std::path::PathBuf;

pub const VERSION: &str = env!("CARGO_PKG_VERSION");

/// Resolved paths for the active install root (`BPM_ROOT`, default `/`).
pub struct Config {
    pub root: String,   // "" for "/", else an absolute dir with no trailing slash
    pub dest: PathBuf,  // real fs target, never "" ("/" when unrooted)
    pub db: PathBuf,
    pub cache: PathBuf,
    pub index: PathBuf,
    pub conf: PathBuf,
    pub provided: PathBuf,
}

impl Config {
    pub fn from_env() -> Config {
        let mut root = std::env::var("BPM_ROOT").unwrap_or_default();
        if root.is_empty() {
            root = "/".to_string();
        }
        // strip trailing slashes; "/" collapses to ""
        while root.len() > 1 && root.ends_with('/') {
            root.pop();
        }
        if root == "/" {
            root.clear();
        }
        let dest = if root.is_empty() {
            PathBuf::from("/")
        } else {
            PathBuf::from(&root)
        };
        let under = |p: &str| PathBuf::from(format!("{root}{p}"));
        Config {
            dest,
            db: under("/var/lib/bpm/db"),
            cache: under("/var/lib/bpm/cache"),
            index: under("/var/lib/bpm/index"),
            conf: under("/etc/bpm/repos.conf"),
            provided: under("/etc/bpm/provided"),
            root,
        }
    }

    /// True when installing into an alternate root (so scriptlets/ldconfig must
    /// chroot into `dest` instead of acting on the host).
    pub fn rooted(&self) -> bool {
        !self.root.is_empty()
    }
}
