// orOS USB: a System button in orOS's top bar with Wi-Fi, sound, battery,
// install to disk and power. It talks to the computer through bg.js and
// the oros-system native host.
(() => {
  if (window.top !== window) return;

  // orOS already is the system here: no browser "Install app" prompt (it
  // only opened an empty window in the kiosk).
  window.addEventListener("beforeinstallprompt", (e) => {
    e.preventDefault();
    e.stopImmediatePropagation();
  }, true);

  const TEXT = {
    en: {
      system: "System", wifi: "Wi-Fi", wired: "Wired network", connectedTo: "Connected to {n}",
      notConnected: "Not connected", wifiOff: "Wi-Fi is off", networks: "Show networks",
      searching: "Searching…", none: "No networks found", password: "Password", connect: "Connect",
      connecting: "Connecting…", connected: "Connected", sound: "Sound", mute: "Mute", unmute: "Unmute",
      noSound: "No sound device found", battery: "Battery {p}%", charging: "charging",
      install: "Install orOS on this computer", installTitle: "Install orOS on this computer",
      installText: "orOS will start from this disk without the USB stick. Everything on the disk you choose is erased: other systems, files and settings.",
      noDisks: "No internal disk of 8 GB or more was found.",
      keepData: "Copy my orOS data from this stick", understand: "I understand that everything on this disk will be erased",
      installNow: "Erase and install", back: "Back", installing: "Installing… do not turn off the computer.",
      installed: "orOS is installed. Shut down, remove the USB stick, then turn the computer on.",
      failed: "Install failed: {e}", restart: "Restart", shutdown: "Shut down", close: "Close",
      error: "Error: {e}", web: "Internet", webTitle: "Open a web browser window (close it to return to orOS)",
    },
    el: {
      system: "Σύστημα", wifi: "Wi-Fi", wired: "Ενσύρματο δίκτυο", connectedTo: "Συνδεδεμένο στο {n}",
      notConnected: "Χωρίς σύνδεση", wifiOff: "Το Wi-Fi είναι κλειστό", networks: "Εμφάνιση δικτύων",
      searching: "Αναζήτηση…", none: "Δεν βρέθηκαν δίκτυα", password: "Κωδικός", connect: "Σύνδεση",
      connecting: "Σύνδεση…", connected: "Συνδέθηκε", sound: "Ήχος", mute: "Σίγαση", unmute: "Ήχος ανοιχτός",
      noSound: "Δεν βρέθηκε συσκευή ήχου", battery: "Μπαταρία {p}%", charging: "φορτίζει",
      install: "Εγκατάσταση του orOS σε αυτόν τον υπολογιστή", installTitle: "Εγκατάσταση του orOS σε αυτόν τον υπολογιστή",
      installText: "Το orOS θα ξεκινά από αυτόν τον δίσκο χωρίς το USB stick. Ό,τι υπάρχει στον δίσκο που θα διαλέξεις σβήνεται: άλλα συστήματα, αρχεία και ρυθμίσεις.",
      noDisks: "Δεν βρέθηκε εσωτερικός δίσκος 8 GB ή μεγαλύτερος.",
      keepData: "Αντιγραφή των δεδομένων μου του orOS από αυτό το stick", understand: "Καταλαβαίνω ότι θα σβηστούν όλα όσα υπάρχουν σε αυτόν τον δίσκο",
      installNow: "Διαγραφή και εγκατάσταση", back: "Πίσω", installing: "Εγκατάσταση… μην κλείσεις τον υπολογιστή.",
      installed: "Το orOS εγκαταστάθηκε. Κλείσε τον υπολογιστή, βγάλε το USB stick και άνοιξέ τον ξανά.",
      failed: "Η εγκατάσταση απέτυχε: {e}", restart: "Επανεκκίνηση", shutdown: "Τερματισμός", close: "Κλείσιμο",
      error: "Σφάλμα: {e}", web: "Internet", webTitle: "Άνοιγμα παραθύρου για το διαδίκτυο (κλείσ' το για να γυρίσεις στο orOS)",
    },
  };
  const lang = () => ((localStorage.getItem("oros-lang") || navigator.language || "en").slice(0, 2) === "el" ? "el" : "en");
  const t = (k, v) => {
    let s = TEXT[lang()][k] || TEXT.en[k] || k;
    for (const [a, b] of Object.entries(v || {})) s = s.replace("{" + a + "}", b);
    return s;
  };

  const call = (msg) => new Promise((resolve) => {
    try {
      chrome.runtime.sendMessage(msg, (res) => {
        const err = chrome.runtime.lastError;
        resolve(res || { ok: false, error: err ? err.message : "no answer" });
      });
    } catch (e) {
      resolve({ ok: false, error: String(e) });
    }
  });

  const ICON = {
    wifi: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M2 8.5a15 15 0 0 1 20 0"/><path d="M5 12a10.5 10.5 0 0 1 14 0"/><path d="M8.5 15.5a5.5 5.5 0 0 1 7 0"/><circle cx="12" cy="19" r="1" fill="currentColor"/></svg>',
    wifiOff: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M2 8.5a15 15 0 0 1 20 0" opacity=".35"/><path d="M5 12a10.5 10.5 0 0 1 14 0" opacity=".35"/><path d="M8.5 15.5a5.5 5.5 0 0 1 7 0" opacity=".35"/><path d="M3 3l18 18"/></svg>',
    wired: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="14" width="18" height="6" rx="1"/><path d="M7 14v-4h10v4M12 10V4"/></svg>',
    power: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 3v9"/><path d="M6.3 7.3a8 8 0 1 0 11.4 0"/></svg>',
    globe: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/></svg>',
    lock: '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/></svg>',
  };

  const CSS = `
    :host { all: initial; }
    .panel { position: fixed; top: 46px; right: 10px; width: 340px; max-height: calc(100vh - 60px); overflow: auto;
      background: #1f1d1a; color: #e8e4dc; border: 1px solid #3a352d; border-radius: 12px; padding: 14px;
      font: 14px/1.4 system-ui, "Noto Sans", sans-serif; box-shadow: 0 12px 40px rgba(0,0,0,.5); z-index: 2147483000; }
    h2 { font-size: 15px; margin: 0 0 10px; color: #c8a96e; font-weight: 600; }
    h3 { font-size: 12px; text-transform: uppercase; letter-spacing: .06em; color: #a69d8e; margin: 14px 0 6px; font-weight: 600; }
    .row { display: flex; align-items: center; gap: 8px; }
    .grow { flex: 1; }
    .muted { color: #a69d8e; font-size: 13px; }
    button { font: inherit; color: #e8e4dc; background: #2c2924; border: 1px solid #3f3a32; border-radius: 8px; padding: 7px 10px; cursor: pointer; }
    button:hover { border-color: #c8a96e; }
    button.primary { background: #c8a96e; color: #1b1a18; border-color: #c8a96e; font-weight: 600; }
    button.danger { background: #8c2f2f; border-color: #8c2f2f; color: #fff; font-weight: 600; }
    button:disabled { opacity: .45; cursor: default; }
    .net { display: flex; align-items: center; gap: 8px; width: 100%; text-align: left; margin: 3px 0; background: #26231f; }
    .net.on { border-color: #c8a96e; }
    input[type=password], input[type=text] { font: inherit; width: 100%; box-sizing: border-box; background: #14120f; color: #e8e4dc;
      border: 1px solid #3f3a32; border-radius: 8px; padding: 7px 9px; margin: 6px 0; }
    input[type=range] { width: 100%; accent-color: #c8a96e; }
    .switch { position: relative; width: 40px; height: 22px; border-radius: 11px; background: #3f3a32; border: none; padding: 0; }
    .switch::after { content: ""; position: absolute; top: 3px; left: 3px; width: 16px; height: 16px; border-radius: 50%; background: #e8e4dc; transition: left .15s; }
    .switch.on { background: #c8a96e; }
    .switch.on::after { left: 21px; }
    .bar { height: 8px; background: #2c2924; border-radius: 4px; overflow: hidden; margin: 8px 0; }
    .bar > div { height: 100%; background: #c8a96e; width: 0; transition: width .4s; }
    label.chk { display: flex; gap: 8px; align-items: flex-start; margin: 8px 0; }
    .msg { margin: 6px 0; font-size: 13px; }
    .msg.err { color: #f0a0a0; }
    .power { display: flex; gap: 8px; margin-top: 14px; }
    [hidden] { display: none !important; }
    .power button { flex: 1; }
  `;

  let state = {};
  let webBtn = null;
  let btn = null, host = null, root = null, panel = null, view = "main", pollTimer = null;

  const el = (tag, attrs, ...kids) => {
    const n = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (k === "class") n.className = v;
      else if (k === "html") n.innerHTML = v;
      else if (k.startsWith("on")) n.addEventListener(k.slice(2), v);
      else if (v !== false && v != null) n.setAttribute(k, v === true ? "" : v);
    }
    for (const kid of kids) if (kid != null) n.append(kid);
    return n;
  };

  function paintButton() {
    if (!btn) return;
    let icon = ICON.wifiOff;
    if (state.wired) icon = ICON.wired;
    else if (state.wifi) icon = ICON.wifi;
    let txt = "";
    if (state.battery) txt = " " + state.battery.percent + "%";
    btn.innerHTML = icon + '<span style="margin-left:4px">' + txt + "</span>";
    btn.title = t("system");
  }

  async function refresh() {
    const r = await call({ cmd: "status" });
    if (r.ok) state = r;
    paintButton();
    if (panel && view === "main" && main) paintMain();
  }

  function close() {
    if (host) host.remove();
    host = root = panel = main = null;
    view = "main";
    clearInterval(pollTimer);
    document.removeEventListener("keydown", onKey, true);
    document.removeEventListener("mousedown", onOutside, true);
  }
  const onKey = (e) => { if (e.key === "Escape" && view !== "installing") close(); };
  const onOutside = (e) => {
    if (view === "installing") return;
    if (host && !e.composedPath().includes(host) && !e.composedPath().includes(btn)) close();
  };

  function open() {
    if (host) { close(); return; }
    host = el("div", { id: "oros-system-panel" });
    root = host.attachShadow({ mode: "open" });
    root.append(el("style", {}, CSS));
    panel = el("div", { class: "panel", role: "dialog", "aria-label": t("system") });
    root.append(panel);
    document.body.append(host);
    document.addEventListener("keydown", onKey, true);
    document.addEventListener("mousedown", onOutside, true);
    renderMain();
    refresh();
  }

  // The main view is built once per opening; refresh() only repaints its
  // status parts, so a scanned network list or a password being typed stays.
  let main = null;

  function renderMain() {
    view = "main";
    panel.replaceChildren();
    const wifiText = el("span", { class: "grow" });
    const sw = el("button", { class: "switch", "aria-label": t("wifi"),
      onclick: async () => { await call({ cmd: "wifi.radio", on: !state.wifiOn }); await refresh(); } });
    const list = el("div", {});
    const nets = el("div", {}, el("div", { class: "row", style: "margin-top:6px" },
      el("button", { onclick: () => scan(list) }, t("networks"))), list);
    const range = el("input", { type: "range", min: "0", max: "100", "aria-label": t("sound") });
    range.addEventListener("change", () => call({ cmd: "volume.set", value: Number(range.value) }).then(refresh));
    const mute = el("button", { onclick: () => call({ cmd: "volume.mute", on: !state.muted }).then(refresh) });
    const soundRow = el("div", { class: "row" }, el("div", { class: "grow" }, range), mute);
    const noSound = el("div", { class: "muted" }, t("noSound"));
    const battery = el("div", { class: "muted", style: "margin-top:12px" });
    const install = el("div", { style: "margin-top:14px" }, el("button", { style: "width:100%", onclick: renderInstall }, t("install")));
    panel.append(el("h2", {}, t("system")),
      el("h3", {}, t("wifi")), el("div", { class: "row" }, wifiText, sw), nets,
      el("h3", {}, t("sound")), soundRow, noSound, battery, install,
      el("div", { class: "power" },
        el("button", { onclick: () => call({ cmd: "power.reboot" }) }, t("restart")),
        el("button", { class: "primary", html: ICON.power + " " + t("shutdown"), onclick: () => call({ cmd: "power.off" }) })));
    main = { wifiText, sw, nets, list, range, mute, soundRow, noSound, battery, install };
    paintMain();
  }

  function paintMain() {
    const m = main;
    let wifiText = t("notConnected");
    if (state.wired) wifiText = t("wired");
    if (state.wifi) wifiText = t("connectedTo", { n: state.wifi.ssid });
    if (state.wifiOn === false) wifiText = t("wifiOff");
    m.wifiText.textContent = wifiText;
    m.sw.className = "switch" + (state.wifiOn ? " on" : "");
    m.sw.setAttribute("aria-pressed", state.wifiOn ? "true" : "false");
    m.nets.hidden = state.wifiOn === false;
    if (state.wifiOn === false) m.list.replaceChildren();
    const hasVolume = typeof state.volume === "number";
    m.soundRow.hidden = !hasVolume;
    m.noSound.hidden = hasVolume;
    if (hasVolume && root.activeElement !== m.range) m.range.value = String(state.volume);
    m.mute.textContent = state.muted ? t("unmute") : t("mute");
    m.battery.hidden = !state.battery;
    if (state.battery) {
      m.battery.textContent = t("battery", { p: state.battery.percent }) + (state.battery.charging ? " · " + t("charging") : "");
    }
    m.install.hidden = !state.canInstall;
  }

  async function scan(list) {
    list.replaceChildren(el("div", { class: "muted" }, t("searching")));
    const r = await call({ cmd: "wifi.scan" });
    list.replaceChildren();
    if (!r.ok) { list.append(el("div", { class: "msg err" }, t("error", { e: r.error }))); return; }
    if (!r.networks || !r.networks.length) { list.append(el("div", { class: "muted" }, t("none"))); return; }
    r.networks.sort((a, b) => (b.inUse - a.inUse) || (b.signal - a.signal));
    for (const n of r.networks) {
      const bars = n.signal >= 70 ? "▂▄▆█" : n.signal >= 45 ? "▂▄▆" : n.signal >= 20 ? "▂▄" : "▂";
      const item = el("button", { class: "net" + (n.inUse ? " on" : "") },
        el("span", { class: "grow" }, n.ssid), n.secure ? el("span", { html: ICON.lock }) : null, el("span", { class: "muted" }, bars));
      const box = el("div", {});
      item.addEventListener("click", () => {
        if (n.inUse) return;
        if (!n.secure) { connect(n.ssid, "", box); return; }
        const pw = el("input", { type: "password", placeholder: t("password"), autocomplete: "off" });
        const go = el("button", { class: "primary", onclick: () => connect(n.ssid, pw.value, box) }, t("connect"));
        pw.addEventListener("keydown", (e) => { if (e.key === "Enter") go.click(); });
        box.replaceChildren(pw, go);
        pw.focus();
      });
      list.append(item, box);
    }
  }

  async function connect(ssid, password, box) {
    box.replaceChildren(el("div", { class: "muted" }, t("connecting")));
    const r = await call({ cmd: "wifi.connect", ssid, password });
    box.replaceChildren(el("div", { class: "msg" + (r.ok ? "" : " err") }, r.ok ? t("connected") : t("error", { e: r.error })));
    refresh();
  }

  async function renderInstall() {
    view = "install";
    panel.replaceChildren(el("h2", {}, t("installTitle")), el("p", {}, t("installText")));
    const r = await call({ cmd: "install.disks" });
    if (!r.ok || !r.disks || !r.disks.length) {
      panel.append(el("div", { class: "msg err" }, r.ok ? t("noDisks") : t("error", { e: r.error })),
        el("button", { onclick: renderMain }, t("back")));
      return;
    }
    let chosen = r.disks[0].name;
    for (const d of r.disks) {
      const gb = Math.round(d.size / 1e9);
      const radio = el("input", { type: "radio", name: "disk", value: d.name, checked: d.name === chosen });
      radio.addEventListener("change", () => { chosen = d.name; });
      panel.append(el("label", { class: "chk" }, radio, el("span", {}, (d.model || d.name) + " · " + gb + " GB" + (d.tran ? " · " + d.tran.toUpperCase() : ""))));
    }
    const keep = el("input", { type: "checkbox", checked: true });
    const sure = el("input", { type: "checkbox" });
    const go = el("button", { class: "danger", disabled: true }, t("installNow"));
    sure.addEventListener("change", () => { go.disabled = !sure.checked; });
    go.addEventListener("click", async () => {
      go.disabled = true;
      const s = await call({ cmd: "install.start", disk: chosen, keepData: keep.checked });
      if (!s.ok) { panel.append(el("div", { class: "msg err" }, t("failed", { e: s.error }))); return; }
      renderProgress();
    });
    panel.append(el("label", { class: "chk" }, keep, el("span", {}, t("keepData"))),
      el("label", { class: "chk" }, sure, el("span", {}, t("understand"))),
      el("div", { class: "row" }, el("button", { onclick: renderMain }, t("back")), el("span", { class: "grow" }), go));
  }

  function renderProgress() {
    view = "installing";
    const fill = el("div", {});
    const msg = el("div", { class: "msg" }, t("installing"));
    panel.replaceChildren(el("h2", {}, t("installTitle")), el("div", { class: "bar" }, fill), msg);
    clearInterval(pollTimer);
    pollTimer = setInterval(async () => {
      const r = await call({ cmd: "install.status" });
      const p = (r && r.progress) || {};
      fill.style.width = (p.percent || 0) + "%";
      if (!p.done) return;
      clearInterval(pollTimer);
      view = "done";
      if (p.error) {
        msg.className = "msg err";
        msg.textContent = t("failed", { e: p.error });
        panel.append(el("button", { onclick: renderMain }, t("back")));
      } else {
        msg.textContent = t("installed");
        panel.append(el("div", { class: "power" },
          el("button", { class: "primary", html: ICON.power + " " + t("shutdown"), onclick: () => call({ cmd: "power.off" }) })));
      }
    }, 1000);
  }

  function mount() {
    const bar = document.querySelector("#oros-bar .bar-right");
    if (!bar || btn) return !!btn;
    btn = el("button", { type: "button", id: "oros-system-btn", "aria-haspopup": "dialog",
      style: "display:inline-flex;align-items:center;gap:2px;background:none;border:0;color:inherit;font:inherit;padding:4px 6px;cursor:pointer", onclick: (e) => { e.stopPropagation(); open(); } });
    // Internet: a normal browser window over orOS, for the web outside orOS.
    webBtn = el("button", { type: "button", id: "oros-web-btn",
      style: "display:inline-flex;align-items:center;gap:4px;background:none;border:0;color:inherit;font:inherit;padding:4px 6px;cursor:pointer",
      html: ICON.globe + "<span>" + t("web") + "</span>", title: t("webTitle"),
      onclick: (e) => { e.stopPropagation(); call({ cmd: "web.open" }); } });
    bar.append(webBtn, btn);
    paintButton();
    refresh();
    setInterval(refresh, 30000);
    return true;
  }

  const start = () => {
    if (mount()) return;
    const mo = new MutationObserver(() => { if (mount()) mo.disconnect(); });
    mo.observe(document.documentElement, { childList: true, subtree: true });
  };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start);
  else start();
})();
