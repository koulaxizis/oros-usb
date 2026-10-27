// Multitasking measurement on the stick (scripts/test-multitask.sh only;
// never in the image). Loaded by a test copy of orOS's index.html. In
// the real shell, three passes, each from a fresh start of orOS:
//   off    the switch off: 6 everyday apps one after the other, then
//          back to each (every app loads again)
//   on     the same with the switch on (apps you leave stay alive)
//   heavy  the switch on, 4 heavy apps (Atelier, Maps, Photo Lab, Layout)
//          among 6
// Each step is printed as "OROS-MT {json}" on the console, which
// Chromium's --enable-logging writes to session.log, and saved at the end
// as ~/Downloads/oros-mt.jsonl; the test reads the
// guest's memory and CPU from outside (QEMU) at the same times.
(function () {
  "use strict";
  if (window.top !== window) return;
  var KEY = "qa-mt-pass";
  var PASSES = [
    { name: "off", mt: false, apps: ["notes", "todo", "writer", "spreadsheet", "files", "budget"] },
    { name: "on", mt: true, apps: ["notes", "todo", "writer", "spreadsheet", "files", "budget"] },
    { name: "heavy", mt: true, apps: ["notes", "writer", "atelier", "maps", "photolab", "layout"] }
  ];
  var IDLE = 60000;

  function log(o) {
    o.t = Date.now();
    var m = performance.memory;
    if (m) o.heapMB = Math.round(m.usedJSHeapSize / 1048576);
    o.frames = document.querySelectorAll("#oros-running iframe").length;
    console.log("OROS-MT " + JSON.stringify(o));
    try { localStorage.setItem("qa-mt-log", (localStorage.getItem("qa-mt-log") || "") + JSON.stringify(o) + "\n"); } catch (e) {}
  }
  // The whole log as a download (~/Downloads), in case the console does
  // not reach session.log.
  function save() {
    var a = document.createElement("a");
    a.href = URL.createObjectURL(new Blob([localStorage.getItem("qa-mt-log") || ""], { type: "application/json" }));
    a.download = "oros-mt.jsonl";
    document.body.appendChild(a);
    a.click();
  }
  function sleep(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
  function appUrl(id) {
    var a = (window.__qaApps || []).filter(function (x) { return x.id === id; })[0];
    return a ? a.url : id + "/";
  }
  function ready(id) {
    var f = document.getElementById("app-frame");
    if (!f || f.style.display === "none") return false;
    try {
      var w = f.contentWindow, d = f.contentDocument;
      if (!w || !d || d.readyState !== "complete") return false;
      if (w.location.pathname.indexOf("/" + appUrl(id)) < 0) return false;
      if (!w.__qaPainted) {
        w.__qaPainted = 1;
        w.requestAnimationFrame(function () { w.requestAnimationFrame(function () { w.__qaPainted = 2; }); });
        return false;
      }
      return w.__qaPainted === 2;
    } catch (e) { return false; }
  }
  function front(id) {
    var t0 = performance.now();
    window.__orosOpenApp(id);
    return new Promise(function (res) {
      (function poll() {
        if (ready(id)) return res(Math.round(performance.now() - t0));
        if (performance.now() - t0 > 90000) return res(-1);
        setTimeout(poll, 50);
      })();
    });
  }
  function kept() {
    try { return !!document.getElementById("app-frame").contentWindow.__qaKeep; } catch (e) { return false; }
  }
  function mark() {
    try { document.getElementById("app-frame").contentWindow.__qaKeep = 1; } catch (e) {}
  }

  async function run(p) {
    log({ ev: "pass", pass: p.name, mt: localStorage.getItem("oros-multitask") === "1", mem: navigator.deviceMemory || null });
    for (var i = 0; i < p.apps.length; i++) {
      var ms = await front(p.apps[i]);
      mark();
      await sleep(4000);
      log({ ev: "open", pass: p.name, app: p.apps[i], ms: ms });
    }
    log({ ev: "idle-start", pass: p.name });
    await sleep(IDLE);
    log({ ev: "idle-end", pass: p.name });
    for (var j = p.apps.length - 2; j >= 0; j--) {
      var ms2 = await front(p.apps[j]);
      log({ ev: "back", pass: p.name, app: p.apps[j], ms: ms2, reloaded: !kept() });
      mark();
      await sleep(3000);
    }
    log({ ev: "pass-end", pass: p.name });
  }

  window.addEventListener("load", function () {
    var n = parseInt(localStorage.getItem(KEY) || "0", 10);
    if (n >= PASSES.length) { log({ ev: "done" }); setTimeout(save, 3000); return; }
    var p = PASSES[n];
    // Each pass starts from a fresh orOS with its switch already set.
    if ((localStorage.getItem("oros-multitask") === "1") !== p.mt) {
      if (p.mt) localStorage.setItem("oros-multitask", "1"); else localStorage.removeItem("oros-multitask");
      location.reload();
      return;
    }
    localStorage.setItem("oros-tour-done", "1");
    // Let the first start settle (service worker, precache) before measuring.
    setTimeout(function () {
      if (typeof window.__orosOpenApp !== "function") { log({ ev: "error", what: "no __orosOpenApp" }); return; }
      fetch("apps.json").then(function (r) { return r.json(); }).then(function (j) { window.__qaApps = j.apps; })
        .catch(function () {}).then(function () { return run(p); })
        .then(function () {
          localStorage.setItem(KEY, String(n + 1));
          return sleep(5000);
        }).then(function () { location.reload(); })
        .catch(function (e) { log({ ev: "error", what: String(e && e.message || e) }); });
    }, n === 0 ? 90000 : 20000);
  });
})();
