// Background service worker: injects the picker content script into the active tab
// on demand (activeTab + scripting), so the extension needs no host permissions.

async function injectAndStart(tabId) {
  try {
    await chrome.scripting.executeScript({
      target: { tabId },
      files: ["content.js"],
    });
    await chrome.tabs.sendMessage(tabId, { type: "track-elem:start" });
    return { ok: true };
  } catch (err) {
    return { ok: false, error: String(err && err.message ? err.message : err) };
  }
}

chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (msg && msg.type === "track-elem:pick") {
    (async () => {
      const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
      if (!tab || tab.id == null) {
        sendResponse({ ok: false, error: "no active tab" });
        return;
      }
      const result = await injectAndStart(tab.id);
      sendResponse(result);
    })();
    return true; // keep the message channel open for the async response
  }
});
