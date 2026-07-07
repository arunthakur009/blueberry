//! bbconsole — the Blueberry management console daemon (base layer).
//!
//! A small, privileged HTTP API + static-file server for a first-party web UI to
//! manage a Blueberry box. This is the FOUNDATION, deliberately scoped: it wraps
//! tools that already exist (systemctl, bpm, /proc) behind a versioned, audited,
//! authenticated API, and serves a single-page frontend. The far vision —
//! containers, logs, updates with btrfs snapshot/rollback, storage, network —
//! extends the /api/v1 surface without reworking this core.
//!
//! Security posture (see doc/WEBUI.md):
//!   * binds 127.0.0.1 by default — TLS + exposure are a reverse proxy's job.
//!   * token→session auth; every API call is re-checked.
//!   * write actions are few, argument-validated, and appended to an audit log.
//!   * pure-std HTTP, hard request-size limits, one request per connection.

mod api;
mod auth;
mod http;

use auth::{Sessions, Throttle};
use http::{Request, Response};
use serde_json::json;
use std::io::{BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::Arc;

use rustls::{ServerConfig, ServerConnection, StreamOwned};

struct Config {
    bind: String,
    web_root: PathBuf,
    token_path: PathBuf,
    audit_path: PathBuf,
    admin_group: String,
    cert_path: PathBuf,
    key_path: PathBuf,
}

impl Config {
    fn load() -> Config {
        // Env overrides keep the base layer configurable without a parser.
        Config {
            bind: env("BBCONSOLE_BIND", "0.0.0.0:9090"),
            web_root: PathBuf::from(env("BBCONSOLE_WEB", "/usr/share/blueberry-console/web")),
            token_path: PathBuf::from(env("BBCONSOLE_TOKEN", "/etc/blueberry/console/token")),
            audit_path: PathBuf::from(env("BBCONSOLE_AUDIT", "/var/log/blueberry-console/audit.log")),
            // Only root + members of this group may log in via PAM.
            admin_group: env("BBCONSOLE_ADMIN_GROUP", "wheel"),
            cert_path: PathBuf::from(env("BBCONSOLE_CERT", "/etc/blueberry/console/cert.pem")),
            key_path: PathBuf::from(env("BBCONSOLE_KEY", "/etc/blueberry/console/key.pem")),
        }
    }
}

fn env(k: &str, default: &str) -> String {
    std::env::var(k).unwrap_or_else(|_| default.to_string())
}

struct State {
    cfg: Config,
    sessions: Sessions,
    throttle: Throttle,
    tls: Arc<ServerConfig>,
}

fn main() {
    // Install the ring crypto provider once for the whole process.
    let _ = rustls::crypto::ring::default_provider().install_default();

    let cfg = Config::load();
    let token = auth::load_or_create_token(&cfg.token_path)
        .unwrap_or_else(|e| { eprintln!("bbconsole: cannot init token: {e}"); std::process::exit(1); });
    ensure_cert(&cfg.cert_path, &cfg.key_path);
    let tls = load_tls(&cfg.cert_path, &cfg.key_path)
        .unwrap_or_else(|| { eprintln!("bbconsole: cannot load TLS cert/key"); std::process::exit(1); });

    let bind = cfg.bind.clone();
    let state = Arc::new(State { cfg, sessions: Sessions::new(token), throttle: Throttle::new(), tls });

    let listener = TcpListener::bind(&bind)
        .unwrap_or_else(|e| { eprintln!("bbconsole: cannot bind {bind}: {e}"); std::process::exit(1); });
    eprintln!("bbconsole: HTTPS on https://{bind} (self-signed cert; PAM login required)");

    for conn in listener.incoming() {
        let Ok(stream) = conn else { continue };
        let st = Arc::clone(&state);
        // Thread per connection: simple and isolated.
        std::thread::spawn(move || handle(st, stream));
    }
}

/// Generate a self-signed cert+key (via openssl) if none exists, so HTTPS works
/// out of the box. Drop in a real cert at the same paths to replace it.
fn ensure_cert(cert: &Path, key: &Path) {
    if cert.exists() && key.exists() {
        return;
    }
    if let Some(dir) = cert.parent() {
        let _ = std::fs::create_dir_all(dir);
    }
    let host = std::fs::read_to_string("/proc/sys/kernel/hostname")
        .map(|s| s.trim().to_string())
        .unwrap_or_else(|_| "blueberry".into());
    let ok = Command::new("openssl")
        .args([
            "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "3650",
            "-keyout", &key.to_string_lossy(),
            "-out", &cert.to_string_lossy(),
            "-subj", &format!("/CN={host}"),
            "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1",
        ])
        .status()
        .map(|s| s.success())
        .unwrap_or(false);
    if ok {
        let _ = std::fs::set_permissions(key, std::os::unix::fs::PermissionsExt::from_mode(0o600));
        eprintln!("bbconsole: generated a self-signed TLS cert at {}", cert.display());
    } else {
        eprintln!("bbconsole: WARNING could not generate a cert (is openssl installed?)");
    }
}

fn load_tls(cert: &Path, key: &Path) -> Option<Arc<ServerConfig>> {
    let certs: Vec<_> = rustls_pemfile::certs(&mut BufReader::new(std::fs::File::open(cert).ok()?))
        .filter_map(Result::ok)
        .collect();
    let key = rustls_pemfile::private_key(&mut BufReader::new(std::fs::File::open(key).ok()?))
        .ok()??;
    let cfg = ServerConfig::builder()
        .with_no_client_auth()
        .with_single_cert(certs, key)
        .ok()?;
    Some(Arc::new(cfg))
}

fn handle(st: Arc<State>, tcp: TcpStream) {
    let peer = tcp.peer_addr().map(|a| a.ip().to_string()).unwrap_or_default();
    // Wrap the connection in TLS; a client speaking plain HTTP just fails here.
    let Ok(conn) = ServerConnection::new(Arc::clone(&st.tls)) else { return };
    let stream = StreamOwned::new(conn, tcp);
    serve(&st, stream, &peer);
}

fn serve<S: Read + Write>(st: &State, stream: S, peer: &str) {
    let mut reader = BufReader::new(stream);
    let Some(req) = http::read_request(&mut reader) else { return };
    let resp = route(st, &req, peer);
    let mut inner = reader.into_inner();
    resp.write(&mut inner);
}

fn authed(st: &State, req: &Request) -> bool {
    req.cookie("bbc_session").and_then(|s| st.sessions.check(&s)).is_some()
}

fn audit(st: &State, ip: &str, line: &str) {
    if let Some(dir) = st.cfg.audit_path.parent() {
        let _ = std::fs::create_dir_all(dir);
    }
    if let Ok(mut f) = std::fs::OpenOptions::new().create(true).append(true).open(&st.cfg.audit_path) {
        let _ = writeln!(f, "{} {} {}", now(), ip, line);
    }
}

fn now() -> String {
    // Seconds since epoch — enough for an audit timeline without a time crate.
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs().to_string())
        .unwrap_or_default()
}

fn route(st: &State, req: &Request, peer: &str) -> Response {
    let path = req.path.as_str();

    // ── auth endpoints ────────────────────────────────────────────────────────
    if path == "/api/v1/login" && req.method == "POST" {
        // Brute-force throttle: too many failures from this IP → back off.
        if !st.throttle.allowed(peer) {
            audit(st, peer, "login THROTTLED");
            return Response::error(429, "too many attempts, try again later");
        }
        let body = req.json().unwrap_or(json!({}));
        let field = |k: &str| body.get(k).and_then(|v| v.as_str()).unwrap_or("").to_string();
        // PAM (username+password) is the primary path; token is the fallback.
        let (sid, who) = if !field("username").is_empty() {
            let user = field("username");
            (
                st.sessions.login_pam(&user, &field("password"), &st.cfg.admin_group),
                user,
            )
        } else {
            (st.sessions.login_token(&field("token")), "token".to_string())
        };
        match sid {
            Some(sid) => {
                st.throttle.clear(peer);
                audit(st, peer, &format!("login ok user={who}"));
                return Response::json(200, json!({ "ok": true, "user": who })).with_header(
                    "Set-Cookie",
                    &format!("bbc_session={sid}; HttpOnly; Secure; SameSite=Strict; Path=/; Max-Age=3600"),
                );
            }
            None => {
                st.throttle.record_fail(peer);
                audit(st, peer, &format!("login FAILED user={who}"));
                return Response::error(401, "invalid credentials");
            }
        }
    }
    if path == "/api/v1/logout" && req.method == "POST" {
        if let Some(sid) = req.cookie("bbc_session") {
            st.sessions.logout(&sid);
        }
        return Response::json(200, json!({ "ok": true }));
    }

    // ── API (all require a session) ───────────────────────────────────────────
    if let Some(rest) = path.strip_prefix("/api/v1/") {
        if !authed(st, req) {
            return Response::error(401, "unauthenticated");
        }
        return api_route(st, req, rest, peer);
    }

    // ── static frontend ───────────────────────────────────────────────────────
    if req.method == "GET" {
        return serve_static(&st.cfg.web_root, path);
    }
    Response::error(404, "not found")
}

fn api_route(st: &State, req: &Request, rest: &str, peer: &str) -> Response {
    match (req.method.as_str(), rest) {
        ("GET", "system") => Response::json(200, api::system()),
        ("GET", "services") => Response::json(200, api::services()),
        ("GET", "packages") => Response::json(200, api::packages()),

        // Write action: /api/v1/services/<action>?unit=<name>
        ("POST", r) if r.starts_with("services/") => {
            let action = &r["services/".len()..];
            let unit = query_param(&req.query, "unit").unwrap_or_default();
            audit(st, peer, &format!("service {action} {unit}"));
            match api::service_action(action, &unit) {
                Ok(v) => Response::json(200, v),
                Err(e) => Response::error(400, &e),
            }
        }

        // Far-vision stubs — stable shape, not built yet.
        ("GET", "containers") => Response::json(501, api::not_implemented("containers")),
        ("GET", "logs") => Response::json(501, api::not_implemented("logs")),
        ("GET", "updates") => Response::json(501, api::not_implemented("updates")),
        ("GET", "storage") => Response::json(501, api::not_implemented("storage")),
        ("GET", "network") => Response::json(501, api::not_implemented("network")),

        _ => Response::error(404, "no such endpoint"),
    }
}

fn query_param(query: &str, key: &str) -> Option<String> {
    for pair in query.split('&') {
        if let Some((k, v)) = pair.split_once('=') {
            if k == key {
                return Some(v.to_string());
            }
        }
    }
    None
}

fn serve_static(root: &Path, path: &str) -> Response {
    let rel = if path == "/" { "index.html" } else { path.trim_start_matches('/') };
    // Path-traversal guard: no "..", no absolute escapes.
    if rel.split('/').any(|c| c == ".." || c.is_empty()) {
        return Response::error(400, "bad path");
    }
    let full = root.join(rel);
    match std::fs::read(&full) {
        Ok(bytes) => Response::bytes(200, content_type(rel), bytes),
        Err(_) => {
            // SPA fallback: unknown non-asset paths return the shell.
            match std::fs::read(root.join("index.html")) {
                Ok(b) => Response::bytes(200, "text/html; charset=utf-8", b),
                Err(_) => Response::error(404, "not found"),
            }
        }
    }
}

fn content_type(name: &str) -> &'static str {
    match name.rsplit('.').next() {
        Some("html") => "text/html; charset=utf-8",
        Some("js") => "text/javascript; charset=utf-8",
        Some("css") => "text/css; charset=utf-8",
        Some("json") => "application/json",
        Some("svg") => "image/svg+xml",
        _ => "application/octet-stream",
    }
}
