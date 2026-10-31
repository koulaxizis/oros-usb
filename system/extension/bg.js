// Passes requests from the orOS page to the oros-system native host.
//
// Two requests are the browser's own business (orOS Portable and the
// desktop apps): "window.mode" switches the sender's window between full
// screen and a window, then lets the host keep the choice for the next
// start; "session.exit" closes every window, so the launcher closes too.
const HOST = "online.useoros.system";

const native = (msg) => new Promise((resolve) => {
  chrome.runtime.sendNativeMessage(HOST, msg, (res) => {
    const err = chrome.runtime.lastError;
    resolve(res || { ok: false, error: err ? err.message : "no answer" });
  });
});

async function windowMode(msg, sender) {
  if (msg.mode !== "fullscreen" && msg.mode !== "window") return { ok: false, error: "unknown mode" };
  if (!sender.tab) return { ok: false, error: "no window" };
  await chrome.windows.update(sender.tab.windowId, { state: msg.mode === "fullscreen" ? "fullscreen" : "maximized" });
  return native(msg);
}

async function exitAll() {
  const wins = await chrome.windows.getAll();
  await Promise.all(wins.map((w) => chrome.windows.remove(w.id).catch(() => {})));
  return { ok: true };
}

// A window that opened before the browser had loaded this extension (the
// first start of a new profile) has no content script: load it again once.
async function reloadBare() {
  const tabs = await chrome.tabs.query({ url: "http://localhost:47800/*" });
  for (const tab of tabs) {
    chrome.tabs.sendMessage(tab.id, { cmd: "oros-here" }, (res) => {
      if (chrome.runtime.lastError || !res) chrome.tabs.reload(tab.id);
    });
  }
}
chrome.runtime.onStartup.addListener(() => setTimeout(reloadBare, 1500));
chrome.runtime.onInstalled.addListener(() => setTimeout(reloadBare, 1500));

chrome.runtime.onMessage.addListener((msg, sender, send) => {
  if (!sender.url || !sender.url.startsWith("http://localhost:47800/")) return false;
  let job;
  if (msg && msg.cmd === "window.mode") job = windowMode(msg, sender);
  else if (msg && msg.cmd === "session.exit") job = exitAll();
  else job = native(msg);
  job.then(send, (e) => send({ ok: false, error: String(e && e.message || e) }));
  return true;
});
