// Passes requests from the orOS page to the oros-system native host.
const HOST = "online.useoros.system";

chrome.runtime.onMessage.addListener((msg, sender, send) => {
  if (!sender.url || !sender.url.startsWith("http://localhost:47800/")) return false;
  chrome.runtime.sendNativeMessage(HOST, msg, (res) => {
    const err = chrome.runtime.lastError;
    send(res || { ok: false, error: err ? err.message : "no answer" });
  });
  return true;
});
