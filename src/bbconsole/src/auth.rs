//! Authentication for the console: a bootstrap admin token exchanged for a
//! short-lived in-memory session cookie.
//!
//! Base-layer model (extensible): on first start the daemon generates a random
//! admin token into a root-only file (`/etc/blueberry/console/token`). The admin
//! reads it (as root) and logs in once; the daemon hands back an opaque session
//! cookie tracked in memory with an idle expiry. Every API call re-checks the
//! session. PAM / per-user accounts + roles are the natural next layer and slot
//! in behind `login()` without changing the API surface.

use std::collections::HashMap;
use std::fs;
use std::io::Read;
use std::os::unix::fs::PermissionsExt;
use std::path::Path;
use std::sync::Mutex;
use std::time::{Duration, Instant};

const SESSION_TTL: Duration = Duration::from_secs(60 * 60); // 1h idle

/// 32 random bytes as lowercase hex, sourced from the kernel CSPRNG.
pub fn random_hex() -> String {
    let mut buf = [0u8; 32];
    let mut f = fs::File::open("/dev/urandom").expect("open /dev/urandom");
    f.read_exact(&mut buf).expect("read /dev/urandom");
    let mut s = String::with_capacity(64);
    for b in buf {
        s.push_str(&format!("{b:02x}"));
    }
    s
}

/// Constant-time-ish equality (length-independent short-circuit avoided).
pub fn ct_eq(a: &str, b: &str) -> bool {
    let (a, b) = (a.as_bytes(), b.as_bytes());
    if a.len() != b.len() {
        return false;
    }
    let mut diff = 0u8;
    for i in 0..a.len() {
        diff |= a[i] ^ b[i];
    }
    diff == 0
}

/// Load the admin token, creating a fresh one (0600) on first run.
pub fn load_or_create_token(path: &Path) -> std::io::Result<String> {
    if let Ok(s) = fs::read_to_string(path) {
        let t = s.trim().to_string();
        if !t.is_empty() {
            return Ok(t);
        }
    }
    if let Some(dir) = path.parent() {
        fs::create_dir_all(dir)?;
    }
    let token = random_hex();
    fs::write(path, format!("{token}\n"))?;
    fs::set_permissions(path, fs::Permissions::from_mode(0o600))?;
    eprintln!("bbconsole: generated a new admin token at {}", path.display());
    Ok(token)
}

pub struct Sessions {
    admin_token: String,
    live: Mutex<HashMap<String, Instant>>, // session id -> last-seen
}

impl Sessions {
    pub fn new(admin_token: String) -> Sessions {
        Sessions { admin_token, live: Mutex::new(HashMap::new()) }
    }

    /// Exchange the admin token for a new session id, or None if it's wrong.
    pub fn login(&self, token: &str) -> Option<String> {
        if !ct_eq(token, &self.admin_token) {
            return None;
        }
        let sid = random_hex();
        self.live.lock().unwrap().insert(sid.clone(), Instant::now());
        Some(sid)
    }

    /// True if `sid` names a live, non-expired session (refreshes its timestamp).
    pub fn check(&self, sid: &str) -> bool {
        let mut live = self.live.lock().unwrap();
        // prune expired
        live.retain(|_, seen| seen.elapsed() < SESSION_TTL);
        if let Some(seen) = live.get_mut(sid) {
            *seen = Instant::now();
            true
        } else {
            false
        }
    }

    pub fn logout(&self, sid: &str) {
        self.live.lock().unwrap().remove(sid);
    }
}
