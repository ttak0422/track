// Popup: triggers the picker in the active tab and shows the hand-off command.
const statusEl = document.getElementById("status");

document.getElementById("pick").addEventListener("click", async () => {
  statusEl.textContent = "";
  statusEl.classList.remove("err");
  const res = await chrome.runtime.sendMessage({ type: "track-elem:pick" });
  if (res && res.ok) {
    window.close();
    return;
  }
  statusEl.textContent = "Failed to start: " + (res && res.error ? res.error : "unknown error");
  statusEl.classList.add("err");
});
