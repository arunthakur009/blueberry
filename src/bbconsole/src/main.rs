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

use auth::Sessions;
use http::{Request, Response};
use serde_json::json;
use std::io::Write;
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::sync::Arc;

struct Config {
    bind: String,
    web_root: PathBuf,
    token_path: PathBuf,
    audit_path: PathBuf,
    admin_group: String,
}

impl Config {
    fn load() -> Config {
        // Env overrides keep the base layer configurable without a parser.
        Config {
            bind: env("BBCONSOLE_BIND", "127.0.0.1:9090"),
            web_root: PathBuf::from(env("BBCONSOLE_WEB", "/usr/share/blueberry-console/web")),
            token_path: PathBuf::from(env("BBCONSOLE_TOKEN", "/etc/blueberry/console/token")),
            audit_path: PathBuf::from(env("BBCONSOLE_AUDIT", "/var/log/blueberry-console/audit.log")),
            // Only root + members of this group may log in via PAM.
            admin_group: env("BBCONSOLE_ADMIN_GROUP", "wheel"),
        }
    }
}

fn env(k: &str, default: &str) -> String {
    std::env::var(k).unwrap_or_else(|_| default.to_string())
}

struct State {
    cfg: Config,
    sessions: Sessions,
}

fn main() {
    let cfg = Config::load();
    let token = auth::load_or_create_token(&cfg.token_path)
        .unwrap_or_else(|e| { eprintln!("bbconsole: cannot init token: {e}"); std::process::exit(1); });
    let sessions = Sessions::new(token);
    let bind = cfg.bind.clone();
    let state = Arc::new(State { cfg, sessions });

    let listener = TcpListener::bind(&bind)
        .unwrap_or_else(|e| { eprintln!("bbconsole: cannot bind {bind}: {e}"); std::process::exit(1); });
    eprintln!("bbconsole: listening on http://{bind} (put TLS/reverse-proxy in front for exposure)");

    for conn in listener.incoming() {
        let Ok(stream) = conn else { continue };
        let st = Arc::clone(&state);
        // Thread per connection: simple and isolated. A bounded pool is a later
        // refinement; management traffic is low-concurrency.
        std::thread::spawn(move || handle(st, stream));
    }
}

fn handle(st: Arc<State>, mut stream: TcpStream) {
    let Some(req) = http::read_request(&stream) else { return };
    let resp = route(&st, &req);
    resp.write(&mut stream);
}

fn authed(st: &State, req: &Request) -> bool {
    req.cookie("bbc_session").and_then(|s| st.sessions.check(&s)).is_some()
}

fn audit(st: &State, req: &Request, line: &str) {
    if let Some(dir) = st.cfg.audit_path.parent() {
        let _ = std::fs::create_dir_all(dir);
    }
    if let Ok(mut f) = std::fs::OpenOptions::new().create(true).append(true).open(&st.cfg.audit_path) {
        let ip = req.header("x-forwarded-for").unwrap_or("local");
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

fn route(st: &State, req: &Request) -> Response {
    let path = req.path.as_str();

    // ── auth endpoints ────────────────────────────────────────────────────────
    if path == "/api/v1/login" && req.method == "POST" {
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
                audit(st, req, &format!("login ok user={who}"));
                return Response::json(200, json!({ "ok": true, "user": who })).with_header(
                    "Set-Cookie",
                    &format!("bbc_session={sid}; HttpOnly; SameSite=Strict; Path=/; Max-Age=3600"),
                );
            }
            None => {
                audit(st, req, &format!("login FAILED user={who}"));
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
        return api_route(st, req, rest);
    }

    // ── static frontend ───────────────────────────────────────────────────────
    if req.method == "GET" {
        return serve_static(&st.cfg.web_root, path);
    }
    Response::error(404, "not found")
}

fn api_route(st: &State, req: &Request, rest: &str) -> Response {
    match (req.method.as_str(), rest) {
        ("GET", "system") => Response::json(200, api::system()),
        ("GET", "services") => Response::json(200, api::services()),
        ("GET", "packages") => Response::json(200, api::packages()),

        // Write action: /api/v1/services/<action>?unit=<name>
        ("POST", r) if r.starts_with("services/") => {
            let action = &r["services/".len()..];
            let unit = query_param(&req.query, "unit").unwrap_or_default();
            audit(st, req, &format!("service {action} {unit}"));
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
