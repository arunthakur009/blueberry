// Blueberry Console — base-layer frontend. Vanilla JS, no build step, no inline
// script (CSP-friendly). Panels are data-driven so far-vision areas (containers,
// logs, updates/rollback, storage, network) drop in by adding an entry to PANELS.

// Auth is a bearer token in sessionStorage (see doc/WEBUI.md security model):
// sent explicitly, so it survives self-signed-cert browsers that drop Secure
// cookies, and is immune to CSRF.
const TOK = "bbc_session";
const tok = () => sessionStorage.getItem(TOK) || "";
const setTok = (t) => { if (t) sessionStorage.setItem(TOK, t); else sessionStorage.removeItem(TOK); };

const api = async (path, opts = {}) => {
  const headers = Object.assign({ "Authorization": "Bearer " + tok() }, opts.headers || {});
  const r = await fetch("/api/v1/" + path, Object.assign({ credentials: "omit" }, opts, { headers }));
  if (r.status === 401) { setTok(""); showLogin(); throw new Error("unauthenticated"); }
  return r;
};
const getJSON = async (path) => (await api(path)).json();
const el = (tag, attrs = {}, ...kids) => {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") n.className = v; else if (k === "text") n.textContent = v; else n.setAttribute(k, v);
  }
  for (const k of kids) n.append(k);
  return n;
};

// ── panels (extend here for the far vision) ───────────────────────────────────
const PANELS = [
  { id: "overview", label: "Overview", render: overview },
  { id: "services", label: "Services", render: services },
  { id: "packages", label: "Packages", render: packages },
  { id: "logs", label: "Logs", render: logs },
  { id: "storage", label: "Storage", render: storage },
  { id: "network", label: "Network", render: network },
  { id: "containers", label: "Containers", render: stub("containers") },
  { id: "updates", label: "Updates", render: stub("updates") },
];

let liveTimer = null; // overview's metrics poller; cleared on navigation/logout

function stub(area) {
  return async (view) => {
    view.append(el("div", { class: "card muted" },
      el("h2", { text: area[0].toUpperCase() + area.slice(1) }),
      el("p", { text: "Planned — this panel is part of the console's roadmap and not built yet." })));
  };
}

// ── formatting helpers ────────────────────────────────────────────────────────
const fmtUptime = (s) => {
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  return [d && d + "d", h && h + "h", m + "m"].filter(Boolean).join(" ");
};
const fmtBytes = (n) => {
  n = Number(n) || 0; const u = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"]; let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return n.toFixed(i ? 1 : 0) + " " + u[i];
};
const fmtTime = (epoch) => (epoch ? new Date(epoch * 1000).toLocaleString() : "—");
const PRIO = ["emerg", "alert", "crit", "err", "warning", "notice", "info", "debug"];
const prioLabel = (p) => PRIO[p] || "info";
const prioClass = (p) => (p <= 3 ? "crit" : p === 4 ? "warn" : "");
const bar = (pct) => {
  const w = el("div", { class: "bar" + (pct >= 90 ? " crit" : pct >= 75 ? " warn" : "") });
  const f = el("span", {}); f.style.width = Math.min(100, pct) + "%"; w.append(f); w.title = pct + "%";
  return w;
};
const stat = (label, value) => el("div", { class: "stat" },
  el("div", { class: "k", text: label }), el("div", { class: "v", text: value }));

async function overview(view) {
  const s = await getJSON("system");
  document.getElementById("hostname").textContent = s.hostname;
  const memPct = s.memory.total_kb ? Math.round((1 - s.memory.available_kb / s.memory.total_kb) * 100) : 0;

  // Live tiles, refreshed from /metrics.
  const cpuV = el("div", { class: "v", text: "…" });
  const memV = el("div", { class: "v", text: memPct + "%" });
  const loadV = el("div", { class: "v", text: (s.load[0] ?? 0).toFixed(2) });
  view.append(el("div", { class: "grid" },
    el("div", { class: "stat" }, el("div", { class: "k", text: "CPU usage" }), cpuV),
    el("div", { class: "stat" }, el("div", { class: "k", text: "Memory" }), memV),
    el("div", { class: "stat" }, el("div", { class: "k", text: "Load (1m)" }), loadV),
  ));
  // Static host facts.
  view.append(el("div", { class: "grid" },
    stat("OS", s.os || "Blueberry Linux"),
    stat("Kernel", s.kernel),
    stat("CPU", (s.cpu && s.cpu.model) || "—"),
    stat("Cores", String((s.cpu && s.cpu.cores) || 0)),
    stat("Uptime", fmtUptime(s.uptime_seconds)),
    stat("Processes", String(s.processes || 0)),
    stat("Swap", s.swap && s.swap.total_kb
      ? `${Math.round((1 - s.swap.free_kb / s.swap.total_kb) * 100)}% of ${(s.swap.total_kb / 1048576).toFixed(1)} GiB`
      : "none"),
  ));

  const tick = async () => {
    try {
      const m = await getJSON("metrics");
      cpuV.textContent = m.cpu_pct.toFixed(0) + "%";
      const mp = m.memory.total_kb ? Math.round((1 - m.memory.available_kb / m.memory.total_kb) * 100) : 0;
      memV.textContent = `${mp}% of ${(m.memory.total_kb / 1048576).toFixed(1)} GiB`;
      loadV.textContent = (m.load[0] ?? 0).toFixed(2);
    } catch (_) { if (liveTimer) { clearInterval(liveTimer); liveTimer = null; } }
  };
  await tick();
  liveTimer = setInterval(tick, 2500);
}

async function logs(view) {
  const card = el("div", { class: "card" });
  const sel = el("select", {});
  [["All", ""], ["Notice+ (≤5)", "5"], ["Warning+ (≤4)", "4"], ["Error+ (≤3)", "3"]].forEach(([label, val]) => {
    const o = el("option", { value: val }); o.textContent = label; sel.append(o);
  });
  const body = el("div", {});
  const load = async () => {
    body.replaceChildren(el("div", { class: "muted", text: "Loading…" }));
    const q = "logs?lines=200" + (sel.value ? "&priority=" + sel.value : "");
    const { entries } = await getJSON(q);
    const rows = entries.slice().reverse().map((e) => el("tr", {},
      el("td", { class: "muted mono", text: fmtTime(e.t) }),
      el("td", {}, el("span", { class: "dot " + prioClass(e.priority) }), document.createTextNode(" " + prioLabel(e.priority))),
      el("td", { class: "mono", text: e.unit || "—" }),
      el("td", { text: e.message }),
    ));
    body.replaceChildren(el("div", { class: "scroll" }, el("table", { class: "list" },
      el("thead", {}, el("tr", {}, ...["Time", "Level", "Unit", "Message"].map((h) => el("th", { text: h })))),
      el("tbody", {}, ...rows))));
  };
  sel.addEventListener("change", load);
  card.append(el("h2", { text: "Logs" }), sel, body);
  view.append(card);
  await load();
}

async function storage(view) {
  const { filesystems, devices } = await getJSON("storage");
  const rows = filesystems.map((f) => el("tr", {},
    el("td", { text: f.mount }),
    el("td", { class: "muted mono", text: f.source }),
    el("td", { text: fmtBytes(f.total) }),
    el("td", { text: fmtBytes(f.used) }),
    el("td", { text: fmtBytes(f.available) }),
    el("td", {}, bar(f.use_pct)),
  ));
  view.append(el("div", { class: "card" },
    el("h2", { text: "Filesystems" }),
    el("div", { class: "scroll" }, el("table", { class: "list" },
      el("thead", {}, el("tr", {}, ...["Mount", "Source", "Size", "Used", "Available", "Use"].map((h) => el("th", { text: h })))),
      el("tbody", {}, ...rows)))));

  if (devices && devices.length) {
    const flat = [];
    const walk = (d, depth) => { flat.push([d, depth]); (d.children || []).forEach((c) => walk(c, depth + 1)); };
    devices.forEach((d) => walk(d, 0));
    const drows = flat.map(([d, depth]) => el("tr", {},
      el("td", { class: "mono", text: " ".repeat(depth * 3) + (d.name || "") }),
      el("td", { class: "muted", text: d.type || "" }),
      el("td", { text: fmtBytes(d.size) }),
      el("td", { class: "muted", text: d.fstype || "" }),
      el("td", { class: "muted mono", text: d.mountpoint || "" }),
    ));
    view.append(el("div", { class: "card" },
      el("h2", { text: "Block devices" }),
      el("div", { class: "scroll" }, el("table", { class: "list" },
        el("thead", {}, el("tr", {}, ...["Name", "Type", "Size", "FS", "Mount"].map((h) => el("th", { text: h })))),
        el("tbody", {}, ...drows)))));
  }
}

async function network(view) {
  const { interfaces, gateway } = await getJSON("network");
  view.append(el("div", { class: "card" },
    el("h2", { text: "Network" }),
    el("p", { class: "muted", text: "Default gateway: " + (gateway || "—") })));
  const rows = interfaces.map((i) => el("tr", {},
    el("td", {}, el("span", { class: "dot " + (i.up ? "ok" : "off") }), document.createTextNode(" " + i.name)),
    el("td", { class: "muted mono", text: i.mac || "—" }),
    el("td", { class: "mono", text: (i.addrs || []).map((a) => a.address).join(", ") || "—" }),
  ));
  view.append(el("div", { class: "card" },
    el("div", { class: "scroll" }, el("table", { class: "list" },
      el("thead", {}, el("tr", {}, ...["Interface", "MAC", "Addresses"].map((h) => el("th", { text: h })))),
      el("tbody", {}, ...rows)))));
}

async function services(view) {
  const { services } = await getJSON("services");
  const act = async (unit, action) => {
    await api("services/" + action + "?unit=" + encodeURIComponent(unit), { method: "POST" });
    render("services");
  };
  const rows = services.map((s) => {
    const running = s.active === "active";
    const btn = (label, action) => { const b = el("button", { class: "ghost sm", text: label }); b.addEventListener("click", () => act(s.unit, action)); return b; };
    return el("tr", {},
      el("td", {}, el("span", { class: "dot " + (running ? "ok" : "off") }), document.createTextNode(" " + s.unit)),
      el("td", { class: "muted", text: s.sub }),
      el("td", { text: s.description }),
      el("td", {}, running ? btn("Stop", "stop") : btn("Start", "start"), btn("Restart", "restart")));
  });
  const table = el("table", { class: "list" }, el("thead", {}, el("tr", {}, el("th", { text: "Unit" }), el("th", { text: "State" }), el("th", { text: "Description" }), el("th", { text: "" }))), el("tbody", {}, ...rows));
  view.append(el("div", { class: "card" }, el("h2", { text: `Services (${services.length})` }), table));
}

async function packages(view) {
  const { packages } = await getJSON("packages");
  const rows = packages.map((p) => el("tr", {}, el("td", { text: p.name }), el("td", { class: "muted", text: p.version })));
  view.append(el("div", { class: "card" },
    el("h2", { text: `Packages (${packages.length})` }),
    el("table", { class: "list" }, el("tbody", {}, ...rows))));
}

// ── shell / routing ───────────────────────────────────────────────────────────
function buildNav() {
  const nav = document.getElementById("nav");
  nav.replaceChildren();
  for (const p of PANELS) {
    const a = el("button", { class: "tab", text: p.label });
    a.dataset.id = p.id;
    a.addEventListener("click", () => render(p.id));
    nav.append(a);
  }
}

async function render(id) {
  if (liveTimer) { clearInterval(liveTimer); liveTimer = null; } // stop the overview poller
  const panel = PANELS.find((p) => p.id === id) || PANELS[0];
  document.querySelectorAll(".tab").forEach((t) => t.classList.toggle("active", t.dataset.id === panel.id));
  const view = document.getElementById("view");
  view.replaceChildren(el("div", { class: "muted", text: "Loading…" }));
  try { view.replaceChildren(); await panel.render(view); }
  catch (e) { view.replaceChildren(el("div", { class: "card error", text: String(e.message || e) })); }
}

function showApp() {
  document.getElementById("login").hidden = true;
  document.getElementById("app").hidden = false;
  buildNav();
  render("overview");
}
function showLogin() {
  if (liveTimer) { clearInterval(liveTimer); liveTimer = null; }
  document.getElementById("app").hidden = true;
  document.getElementById("login").hidden = false;
}

document.getElementById("login-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const err = document.getElementById("login-error");
  err.hidden = true;
  let r;
  try {
    r = await fetch("/api/v1/login", {
      method: "POST", credentials: "omit",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        username: document.getElementById("username").value,
        password: document.getElementById("password").value,
      }),
    });
  } catch (netErr) {
    err.textContent = "Network error: " + ((netErr && netErr.message) || netErr); err.hidden = false; return;
  }
  let data = {};
  try { data = await r.json(); } catch (_) {}
  if (r.ok && data.session) { setTok(data.session); showApp(); }
  else {
    err.textContent = r.status === 429
      ? "Too many attempts — try again shortly."
      : "Invalid credentials, or account not permitted. (HTTP " + r.status + ")";
    err.hidden = false;
  }
});

document.getElementById("logout").addEventListener("click", async () => {
  try { await api("logout", { method: "POST" }); } catch (_) {}
  setTok(); showLogin();
});

// Probe an existing session on load (only if we hold a token).
(async () => {
  if (!tok()) return showLogin();
  try {
    const r = await fetch("/api/v1/system", { headers: { "Authorization": "Bearer " + tok() } });
    if (r.ok) showApp(); else { setTok(""); showLogin(); }
  } catch (_) { showLogin(); }
})();
