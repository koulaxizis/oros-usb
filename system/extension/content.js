// orOS USB: a System button in orOS's top bar with Wi-Fi, sound, battery,
// install to disk and power. It talks to the computer through bg.js and
// the oros-system native host.
(() => {
  if (window.top !== window) return;

  // orOS's on-screen keyboard (osk.js) reads this: the kiosk has no system
  // keyboard, so on a touch screen it opens by itself and its top-bar
  // button is always there.
  document.documentElement.setAttribute("data-oros-kiosk", "usb");

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
      searching: "Searching…", none: "No networks found", password: "Password", showPw: "Show password", hidePw: "Hide password", connect: "Connect",
      connecting: "Connecting…", connected: "Connected", sound: "Sound", mute: "Mute", unmute: "Unmute",
      noSound: "No sound device found", battery: "Battery {p}%", charging: "charging",
      install: "Install orOS on this computer", installTitle: "Install orOS on this computer",
      installText: "orOS will start from this disk without the USB stick. Everything on the disk you choose is erased: other systems, files and settings.",
      noDisks: "No internal disk of 8 GB or more was found.",
      keepData: "Copy my orOS data from this stick", understand: "I understand that everything on this disk will be erased",
      installNow: "Erase and install", back: "Back", installing: "Installing… do not turn off the computer.",
      installed: "orOS is installed. Shut down, remove the USB stick, then turn the computer on.",
      failed: "Install failed: {e}", restart: "Restart", shutdown: "Shut down", close: "Close",
      disks: "Disks", downloads: "Downloads", diskInternal: "This computer", diskUsb: "USB",
      diskOpen: "Open", diskClose: "Close", diskNone: "No other disks. A USB stick appears here when you plug it in.",
      disksHint: "Disks open read-only in the Internet window. orOS's own files are in the Files app.",
      report: "Problem report", reportMaking: "Preparing the report…", reportSaved: "Saved in Downloads: {f}. You can attach it to a message from the Internet window.",
      update: "System update", updateCheck: "Check for updates", updateNone: "This is the newest system ({v}).",
      updateAvail: "New system {v} ({mb} MB). Your files and settings stay.", updateNow: "Download and install",
      updating: "Downloading the new system… {p}%", updated: "Done. Restart to start the new system.",
      updateNoPersist: "Updates need a persistence partition on the stick.",
      error: "Error: {e}", web: "Internet", webTitle: "Open a web browser window (close it to return to orOS)",
      screen: "Screen", brightness: "Brightness", time: "Time", timeZone: "Time zone",
      timeSynced: "The clock is set from the internet.", timeNotSynced: "The clock is set from the internet once you are online.",
      timeZoneInstall: "Time zone of this computer",
      bt: "Bluetooth", btFind: "Find devices", btSearching: "Searching for 10 seconds… put the device in pairing mode.",
      btNone: "No new devices found. Is the device in pairing mode?", btPair: "Pair", btPairing: "Pairing…",
      btPasskey: "Type {k} on the keyboard, then press Enter.", btConnect: "Connect", btDisconnect: "Disconnect",
      btForget: "Forget", btConnected: "connected", btNoPaired: "No paired devices.", btOff: "Bluetooth is off",
      printers: "Printers", printTest: "Test page", printSent: "Test page sent to {p}.",
      printNone: "No printers yet. Printers that support AirPrint or IPP Everywhere appear by themselves when they are on the same network or plugged in by USB.",
      printHint: "Print from any app with Ctrl+P.",
    },
    el: {
      system: "Σύστημα", wifi: "Wi-Fi", wired: "Ενσύρματο δίκτυο", connectedTo: "Συνδεδεμένο στο {n}",
      notConnected: "Χωρίς σύνδεση", wifiOff: "Το Wi-Fi είναι κλειστό", networks: "Εμφάνιση δικτύων",
      searching: "Αναζήτηση…", none: "Δεν βρέθηκαν δίκτυα", password: "Κωδικός", showPw: "Εμφάνιση κωδικού", hidePw: "Απόκρυψη κωδικού", connect: "Σύνδεση",
      connecting: "Σύνδεση…", connected: "Συνδέθηκε", sound: "Ήχος", mute: "Σίγαση", unmute: "Ήχος ανοιχτός",
      noSound: "Δεν βρέθηκε συσκευή ήχου", battery: "Μπαταρία {p}%", charging: "φορτίζει",
      install: "Εγκατάσταση του orOS σε αυτόν τον υπολογιστή", installTitle: "Εγκατάσταση του orOS σε αυτόν τον υπολογιστή",
      installText: "Το orOS θα ξεκινά από αυτόν τον δίσκο χωρίς το USB stick. Ό,τι υπάρχει στον δίσκο που θα διαλέξεις σβήνεται: άλλα συστήματα, αρχεία και ρυθμίσεις.",
      noDisks: "Δεν βρέθηκε εσωτερικός δίσκος 8 GB ή μεγαλύτερος.",
      keepData: "Αντιγραφή των δεδομένων μου του orOS από αυτό το stick", understand: "Καταλαβαίνω ότι θα σβηστούν όλα όσα υπάρχουν σε αυτόν τον δίσκο",
      installNow: "Διαγραφή και εγκατάσταση", back: "Πίσω", installing: "Εγκατάσταση… μην κλείσεις τον υπολογιστή.",
      installed: "Το orOS εγκαταστάθηκε. Κλείσε τον υπολογιστή, βγάλε το USB stick και άνοιξέ τον ξανά.",
      failed: "Η εγκατάσταση απέτυχε: {e}", restart: "Επανεκκίνηση", shutdown: "Τερματισμός", close: "Κλείσιμο",
      disks: "Δίσκοι", downloads: "Λήψεις", diskInternal: "Αυτός ο υπολογιστής", diskUsb: "USB",
      diskOpen: "Άνοιγμα", diskClose: "Κλείσιμο", diskNone: "Δεν υπάρχουν άλλοι δίσκοι. Ένα USB stick εμφανίζεται εδώ μόλις το συνδέσεις.",
      disksHint: "Οι δίσκοι ανοίγουν μόνο για ανάγνωση στο παράθυρο Internet. Τα αρχεία του ίδιου του orOS είναι στην εφαρμογή Αρχεία.",
      report: "Αναφορά προβλήματος", reportMaking: "Ετοιμάζεται η αναφορά…", reportSaved: "Αποθηκεύτηκε στις Λήψεις: {f}. Μπορείς να το επισυνάψεις σε μήνυμα από το παράθυρο Internet.",
      update: "Ενημέρωση συστήματος", updateCheck: "Έλεγχος για ενημερώσεις", updateNone: "Έχεις το νεότερο σύστημα ({v}).",
      updateAvail: "Νέο σύστημα {v} ({mb} MB). Τα αρχεία και οι ρυθμίσεις σου μένουν.", updateNow: "Λήψη και εγκατάσταση",
      updating: "Λήψη του νέου συστήματος… {p}%", updated: "Έτοιμο. Κάνε επανεκκίνηση για να ξεκινήσει το νέο σύστημα.",
      updateNoPersist: "Οι ενημερώσεις χρειάζονται διαμέρισμα persistence στο stick.",
      error: "Σφάλμα: {e}", web: "Internet", webTitle: "Άνοιγμα παραθύρου για το διαδίκτυο (κλείσ' το για να γυρίσεις στο orOS)",
      screen: "Οθόνη", brightness: "Φωτεινότητα", time: "Ώρα", timeZone: "Ζώνη ώρας",
      timeSynced: "Η ώρα ρυθμίζεται από το διαδίκτυο.", timeNotSynced: "Η ώρα θα ρυθμιστεί από το διαδίκτυο μόλις συνδεθείς.",
      timeZoneInstall: "Ζώνη ώρας αυτού του υπολογιστή",
      bt: "Bluetooth", btFind: "Εύρεση συσκευών", btSearching: "Αναζήτηση για 10 δευτερόλεπτα… βάλε τη συσκευή σε λειτουργία σύζευξης.",
      btNone: "Δεν βρέθηκαν νέες συσκευές. Είναι η συσκευή σε λειτουργία σύζευξης;", btPair: "Σύζευξη", btPairing: "Σύζευξη…",
      btPasskey: "Πληκτρολόγησε {k} στο πληκτρολόγιο και πάτα Enter.", btConnect: "Σύνδεση", btDisconnect: "Αποσύνδεση",
      btForget: "Διαγραφή", btConnected: "συνδεδεμένο", btNoPaired: "Δεν υπάρχουν συζευγμένες συσκευές.", btOff: "Το Bluetooth είναι κλειστό",
      printers: "Εκτυπωτές", printTest: "Δοκιμαστική σελίδα", printSent: "Η δοκιμαστική σελίδα στάλθηκε στον {p}.",
      printNone: "Δεν υπάρχουν ακόμη εκτυπωτές. Όσοι υποστηρίζουν AirPrint ή IPP Everywhere εμφανίζονται μόνοι τους όταν είναι στο ίδιο δίκτυο ή συνδεθούν με USB.",
      printHint: "Εκτύπωση από κάθε εφαρμογή με Ctrl+P.",
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
    eye: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/></svg>',
    eyeOff: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/><path d="M3 3l18 18"/></svg>',
    drive: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M5 13l2.5-8h9L19 13"/><circle cx="17" cy="16.5" r=".8" fill="currentColor"/></svg>',
    usb: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="7" y="9" width="10" height="13" rx="2"/><path d="M9 9V3h6v6M11 5.5h0M13 5.5h0"/></svg>',
    folder: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M3 6.5A1.5 1.5 0 0 1 4.5 5H9l2 2h8.5A1.5 1.5 0 0 1 21 8.5v9a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 17.5z"/></svg>',
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
    .disk { display: flex; align-items: center; gap: 8px; margin: 4px 0; }
    .disk .name { flex: 1; min-width: 0; }
    .disk .name div { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .disk button { padding: 5px 10px; }
    .pw { display: flex; align-items: center; gap: 6px; }
    .pw button { padding: 6px 8px; line-height: 0; }
    input[type=range] { width: 100%; accent-color: #c8a96e; }
    select { font: inherit; width: 100%; box-sizing: border-box; background: #14120f; color: #e8e4dc; border: 1px solid #3f3a32; border-radius: 8px; padding: 7px 8px; }
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

  // setInterval for async checks: a tick waits while the last one is still
  // waiting for its answer, so a slow answer never runs the end twice.
  const poll = (fn, ms) => {
    let busy = false;
    return setInterval(async () => {
      if (busy) return;
      busy = true;
      try { await fn(); } finally { busy = false; }
    }, ms);
  };
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
    // Brightness: only on screens with a backlight (laptops).
    const light = el("input", { type: "range", min: "5", max: "100", "aria-label": t("brightness") });
    // Follows the slider while it moves, one call at a time.
    let lightBusy = false;
    const sendLight = async () => {
      if (lightBusy) return;
      lightBusy = true;
      const v = light.value;
      await call({ cmd: "brightness.set", value: Number(v) });
      lightBusy = false;
      if (light.value !== v) sendLight();
    };
    light.addEventListener("input", sendLight);
    const screenBox = el("div", {}, el("h3", {}, t("screen")), el("div", { class: "row" }, el("span", {}, t("brightness")), el("div", { class: "grow" }, light)));
    const timeBox = el("div", {});
    const btBox = el("div", {});
    const printBox = el("div", {});
    const install = el("div", { style: "margin-top:14px" }, el("button", { style: "width:100%", onclick: renderInstall }, t("install")));
    const disksBox = el("div", {});
    const updateBox = el("div", {});
    const reportMsg = el("div", {});
    const reportBtn = el("button", { style: "width:100%", onclick: async () => {
      reportBtn.disabled = true;
      reportMsg.replaceChildren(el("div", { class: "muted" }, t("reportMaking")));
      const r = await call({ cmd: "report" });
      reportBtn.disabled = false;
      reportMsg.replaceChildren(el("div", { class: r.ok ? "msg" : "msg err" }, r.ok ? t("reportSaved", { f: r.file }) : t("error", { e: r.error })));
    } }, t("report"));
    panel.append(el("h2", {}, t("system")),
      el("h3", {}, t("wifi")), el("div", { class: "row" }, wifiText, sw), nets,
      el("h3", {}, t("sound")), soundRow, noSound, battery, screenBox,
      btBox,
      el("h3", {}, t("printers")), printBox,
      el("h3", {}, t("time")), timeBox,
      el("h3", {}, t("disks")), disksBox,
      el("h3", {}, t("update")), updateBox, install,
      el("div", { style: "margin-top:10px" }, reportBtn, reportMsg),
      el("div", { class: "power" },
        el("button", { onclick: () => call({ cmd: "power.reboot" }) }, t("restart")),
        el("button", { class: "primary", html: ICON.power + " " + t("shutdown"), onclick: () => call({ cmd: "power.off" }) })));
    main = { wifiText, sw, nets, list, range, mute, soundRow, noSound, battery, install, light, screenBox };
    paintMain();
    showTime(timeBox, false);
    showBluetooth(btBox);
    showPrinters(printBox);
    showUpdate(updateBox);
    showDisks(disksBox);
  }

  // Disks: Downloads, the computer's own disks and USB sticks. Opening one
  // mounts it read-only and shows it in the Internet window. The list
  // follows sticks being plugged in while the panel is open.
  function showDisks(box) {
    const list = el("div", {});
    const msg = el("div", {});
    box.replaceChildren(list, msg, el("div", { class: "muted", style: "font-size:12px;margin-top:4px" }, t("disksHint")));
    const gb = (n) => { const g = n / 1e9; return (g < 10 ? g.toFixed(1) : Math.round(g)) + " GB"; };
    const row = (icon, title, sub, ...buttons) => el("div", { class: "disk" }, el("span", { html: icon }),
      el("div", { class: "name" }, el("div", {}, title), sub ? el("div", { class: "muted", style: "font-size:12px" }, sub) : null), ...buttons);
    const act = async (cmd, path) => {
      msg.replaceChildren();
      const r = await call({ cmd, disk: path });
      if (!r.ok) msg.replaceChildren(el("div", { class: "msg err" }, t("error", { e: r.error })));
      last = "";
      load();
    };
    let last = "";
    async function load() {
      if (!box.isConnected) { clearInterval(timer); return; }
      const r = await call({ cmd: "disks.list" });
      const key = JSON.stringify(r);
      if (key === last) return;
      last = key;
      const rows = [row(ICON.folder, t("downloads"), "", el("button", { onclick: () => act("disks.open", "downloads") }, t("diskOpen")))];
      if (!r.ok) rows.push(el("div", { class: "msg err" }, t("error", { e: r.error })));
      for (const v of (r.volumes || [])) {
        const where = v.usb ? t("diskUsb") : t("diskInternal");
        const title = v.label || v.model || v.path.replace("/dev/", "");
        const sub = [where, title === v.model ? "" : v.model, gb(v.size), v.fstype.toUpperCase()].filter(Boolean).join(" · ");
        const buttons = [el("button", { onclick: () => act("disks.open", v.path) }, t("diskOpen"))];
        if (v.mount) buttons.push(el("button", { onclick: () => act("disks.close", v.path) }, t("diskClose")));
        rows.push(row(v.usb ? ICON.usb : ICON.drive, title, sub, ...buttons));
      }
      if (r.ok && !(r.volumes || []).length) rows.push(el("div", { class: "muted" }, t("diskNone")));
      list.replaceChildren(...rows);
    }
    const timer = setInterval(load, 4000);
    load();
  }

  // Time zone: kept on the stick, copied when orOS is installed. The clock
  // itself comes from the internet (NTP).
  async function showTime(box, forInstall) {
    const r = await call({ cmd: "time.get" });
    if (!r.ok) { box.replaceChildren(el("div", { class: "msg err" }, t("error", { e: r.error }))); return; }
    const sel = el("select", { "aria-label": t("timeZone") });
    for (const z of r.zones || []) sel.append(el("option", { value: z, selected: z === r.zone }, z.replace(/_/g, " ")));
    const msg = el("div", {});
    sel.addEventListener("change", async () => {
      msg.replaceChildren();
      const s = await call({ cmd: "time.zone", zone: sel.value });
      if (!s.ok) msg.replaceChildren(el("div", { class: "msg err" }, t("error", { e: s.error })));
    });
    box.replaceChildren(el("div", { class: forInstall ? "" : "muted", style: "margin-bottom:4px" }, forInstall ? t("timeZoneInstall") : t("timeZone")), sel, msg);
    if (!forInstall) box.append(el("div", { class: "muted", style: "font-size:12px;margin-top:4px" }, r.synced ? t("timeSynced") : t("timeNotSynced")));
  }

  // Bluetooth: paired devices with connect / disconnect / forget, and a
  // search that pairs new ones (showing the code a keyboard asks for).
  // Hidden on computers without Bluetooth.
  async function showBluetooth(box) {
    const r = await call({ cmd: "bt.list" });
    if (!box.isConnected) return;
    if (!r.ok || !r.present) { box.replaceChildren(); return; }
    const msg = el("div", {});
    const err = (e) => msg.replaceChildren(el("div", { class: "msg err" }, t("error", { e })));
    const sw = el("button", { class: "switch" + (r.powered ? " on" : ""), "aria-label": t("bt"), "aria-pressed": r.powered ? "true" : "false",
      onclick: async () => { const s = await call({ cmd: "bt.power", on: !r.powered }); if (!s.ok) err(s.error); showBluetooth(box); } });
    const head = el("div", { class: "row" }, el("h3", { class: "grow" }, t("bt")), sw);
    if (!r.powered) { box.replaceChildren(head, el("div", { class: "muted" }, t("btOff")), msg); return; }
    const act = async (cmd, mac) => {
      msg.replaceChildren(el("div", { class: "muted" }, "…"));
      const s = await call({ cmd, mac });
      if (!s.ok) { err(s.error); return; }
      msg.replaceChildren();
      showBluetooth(box);
    };
    const rows = (r.devices || []).map((d) => el("div", { class: "disk" },
      el("div", { class: "name" }, el("div", {}, d.name), d.connected ? el("div", { class: "muted", style: "font-size:12px" }, t("btConnected")) : null),
      el("button", { onclick: () => act(d.connected ? "bt.disconnect" : "bt.connect", d.mac) }, d.connected ? t("btDisconnect") : t("btConnect")),
      el("button", { onclick: () => act("bt.forget", d.mac) }, t("btForget"))));
    if (!rows.length) rows.push(el("div", { class: "muted" }, t("btNoPaired")));
    const found = el("div", {});
    const find = el("button", { onclick: async () => {
      find.disabled = true;
      found.replaceChildren(el("div", { class: "muted" }, t("btSearching")));
      const s = await call({ cmd: "bt.scan" });
      find.disabled = false;
      if (!s.ok) { found.replaceChildren(); err(s.error); return; }
      const list = (s.devices || []).map((d) => el("div", { class: "disk" }, el("div", { class: "name" }, el("div", {}, d.name)),
        el("button", { class: "primary", onclick: () => pair(d) }, t("btPair"))));
      found.replaceChildren(...(list.length ? list : [el("div", { class: "muted" }, t("btNone"))]));
    } }, t("btFind"));
    const pair = async (d) => {
      const status = el("div", { class: "msg" }, t("btPairing") + " " + d.name);
      found.replaceChildren(status);
      const s = await call({ cmd: "bt.pair", mac: d.mac });
      if (!s.ok) { found.replaceChildren(); err(s.error); return; }
      const started = Date.now();
      const timer = poll(async () => {
        if (!box.isConnected || Date.now() - started > 90000) { clearInterval(timer); return; }
        const p = await call({ cmd: "bt.pairStatus" });
        if (p.passkey && !p.done) status.textContent = t("btPasskey", { k: p.passkey });
        if (!p.done) return;
        clearInterval(timer);
        found.replaceChildren();
        if (p.error) { err(p.error); return; }
        act("bt.connect", d.mac);
      }, 1000);
    };
    box.replaceChildren(head, ...rows, el("div", { class: "row", style: "margin-top:6px" }, find), found, msg);
  }

  // Printers: driverless only (AirPrint / IPP Everywhere), found by
  // themselves; a test page checks one works.
  async function showPrinters(box) {
    const r = await call({ cmd: "printers.list" });
    if (!box.isConnected) return;
    const msg = el("div", {});
    if (!r.ok) { box.replaceChildren(el("div", { class: "msg err" }, t("error", { e: r.error }))); return; }
    const list = r.printers || [];
    if (!list.length) { box.replaceChildren(el("div", { class: "muted" }, t("printNone"))); return; }
    const rows = list.map((p) => el("div", { class: "disk" }, el("div", { class: "name" }, el("div", { title: p.name.replace(/_/g, " ") }, p.name.replace(/_/g, " "))),
      el("button", { onclick: async () => {
        const s = await call({ cmd: "printers.test", printer: p.name });
        msg.replaceChildren(el("div", { class: s.ok ? "msg" : "msg err" }, s.ok ? t("printSent", { p: p.name.replace(/_/g, " ") }) : t("error", { e: s.error })));
      } }, t("printTest"))));
    box.replaceChildren(...rows, msg, el("div", { class: "muted", style: "font-size:12px;margin-top:4px" }, t("printHint")));
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
    m.screenBox.hidden = typeof state.brightness !== "number";
    if (typeof state.brightness === "number" && root.activeElement !== m.light) m.light.value = String(state.brightness);
  }

  // System update: check the latest orOS USB release, download it onto the
  // persistence partition, then a restart starts it.
  async function showUpdate(box) {
    const st = await call({ cmd: "update.status" });
    if (st.ok && st.running) return followUpdate(box);
    if (st.ok && st.progress && st.progress.done && !st.progress.error) {
      box.replaceChildren(el("div", { class: "msg" }, t("updated")));
      return;
    }
    box.replaceChildren(el("div", { class: "row" }, el("button", { onclick: () => checkUpdate(box) }, t("updateCheck"))));
    if (st.ok && st.progress && st.progress.error) box.append(el("div", { class: "msg err" }, t("error", { e: st.progress.error })));
  }

  async function checkUpdate(box) {
    box.replaceChildren(el("div", { class: "muted" }, t("searching")));
    const r = await call({ cmd: "update.check" });
    if (!r.ok) { box.replaceChildren(el("div", { class: "msg err" }, t("error", { e: r.error }))); return; }
    if (!r.available) { box.replaceChildren(el("div", { class: "muted" }, t("updateNone", { v: r.current || "?" }))); return; }
    box.replaceChildren(el("div", {}, t("updateAvail", { v: r.latest, mb: Math.round(r.size / 1048576) })));
    if (!r.canUpdate) { box.append(el("div", { class: "msg err" }, t("updateNoPersist"))); return; }
    box.append(el("div", { class: "row", style: "margin-top:6px" }, el("button", { class: "primary", onclick: async () => {
      const s = await call({ cmd: "update.start" });
      if (!s.ok) { box.append(el("div", { class: "msg err" }, t("error", { e: s.error }))); return; }
      followUpdate(box);
    } }, t("updateNow"))));
  }

  function followUpdate(box) {
    const msg = el("div", { class: "muted" }, t("updating", { p: 0 }));
    const bar = el("progress", { max: "100", value: "0", style: "width:100%" });
    box.replaceChildren(msg, bar);
    const timer = poll(async () => {
      if (!box.isConnected) { clearInterval(timer); return; }
      const st = await call({ cmd: "update.status" });
      const p = (st.progress) || {};
      bar.value = p.percent || 0;
      msg.textContent = t("updating", { p: p.percent || 0 });
      if (!st.running && p.done) {
        clearInterval(timer);
        box.replaceChildren(el("div", { class: p.error ? "msg err" : "msg" }, p.error ? t("error", { e: p.error }) : t("updated")));
      }
    }, 1500);
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
        const eye = el("button", { type: "button", class: "eye", title: t("showPw"), "aria-label": t("showPw"), "aria-pressed": "false", html: ICON.eye });
        eye.addEventListener("click", () => {
          const show = pw.type === "password";
          pw.type = show ? "text" : "password";
          eye.innerHTML = show ? ICON.eyeOff : ICON.eye;
          eye.title = t(show ? "hidePw" : "showPw");
          eye.setAttribute("aria-label", eye.title);
          eye.setAttribute("aria-pressed", String(show));
          pw.focus();
        });
        box.replaceChildren(el("div", { class: "pw" }, pw, eye), go);
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
    const zoneBox = el("div", { style: "margin:10px 0" });
    showTime(zoneBox, true);
    panel.append(zoneBox, el("label", { class: "chk" }, keep, el("span", {}, t("keepData"))),
      el("label", { class: "chk" }, sure, el("span", {}, t("understand"))),
      el("div", { class: "row" }, el("button", { onclick: renderMain }, t("back")), el("span", { class: "grow" }), go));
  }

  function renderProgress() {
    view = "installing";
    const fill = el("div", {});
    const msg = el("div", { class: "msg" }, t("installing"));
    panel.replaceChildren(el("h2", {}, t("installTitle")), el("div", { class: "bar" }, fill), msg);
    clearInterval(pollTimer);
    pollTimer = poll(async () => {
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

  // Sign of life for oros-session's watchdog: the first ping waits for a
  // painted frame (a window that never paints sends none), then one every
  // 10 s while the page runs. A timer, not frames, keeps it going while the
  // Internet window covers orOS.
  const ping = () => call({ cmd: "ping" });
  requestAnimationFrame(() => requestAnimationFrame(() => {
    ping();
    setInterval(ping, 10000);
  }));

  const start = () => {
    if (mount()) return;
    const mo = new MutationObserver(() => { if (mount()) mo.disconnect(); });
    mo.observe(document.documentElement, { childList: true, subtree: true });
  };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start);
  else start();
})();
