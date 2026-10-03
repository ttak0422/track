// Runs only in the disposable WKWebView harness. No React internals or mocked network.
(() => {
  const original = 'WK3 original body';
  const draft = 'WK3 unsaved draft\n日本語 café — second line\n';
  const finalBody = draft + 'Edited after retry.';
  const editor = () => document.querySelector('form.note-editor textarea[aria-label="Note body"]');
  const save = () => document.querySelector('form.note-editor button[type="submit"]');
  const assert = (ok, message) => { if (!ok) throw new Error(message); };
  const wait = async (predicate, message) => {
    const deadline = Date.now() + 12000;
    while (Date.now() < deadline) {
      if (predicate()) return;
      await new Promise(resolve => setTimeout(resolve, 50));
    }
    throw new Error(message + '; UI=' + document.body.innerText.slice(0, 1500));
  };
  const mode = async label => {
    document.querySelector('button[aria-label^="Display mode:"]').click();
    await wait(() => document.querySelector('[aria-label="Display mode"]'), 'mode menu absent');
    const button = [...document.querySelectorAll('[role="menuitemradio"]')]
      .find(button => button.textContent.trim() === label);
    assert(button, `missing ${label} mode`);
    button.click();
    await wait(() => label === 'Preview' ? !editor() : editor(), 'mode did not change');
  };
  const type = value => {
    // Native setter bypasses React's DOM value tracker; input exercises its real onChange handler.
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(editor(), value);
    editor().dispatchEvent(new Event('input', { bubbles: true }));
  };
  const bodyOnServer = async () => {
    const response = await fetch('/api/note?id=100', { cache: 'no-store' });
    assert(response.ok, `note GET ${response.status}`);
    return (await response.json()).note.body;
  };
  const checkDraft = () => {
    assert(window.__wk3Document === 'same-document', 'document replaced');
    assert(editor()?.value === draft, 'React draft lost');
    assert(document.querySelector('.tab.active.dirty'), 'React dirty marker lost');
    assert(save() && !save().disabled, 'Save must remain enabled');
  };
  const stages = {
    async prepare() {
      const vaults = await fetch('/api/vaults', { cache: 'no-store' });
      assert(vaults.ok && (await vaults.json()).active?.path === window.__wk3ExpectedVault,
        'server is not using the isolated fixture vault');
      await wait(() => document.querySelector('button[aria-label^="Display mode:"]'), 'note not loaded');
      await mode('Edit');
      await wait(() => editor()?.value === original, 'original body not adopted');
      window.__wk3Document = 'same-document';
      type(draft);
      await wait(() => document.querySelector('.tab.active.dirty') && !save().disabled, 'React did not adopt input');
      // Remount the textarea: a DOM-only value assignment cannot pass this check.
      await mode('Preview');
      await mode('Edit');
      checkDraft();
      assert(await bodyOnServer() === original, 'draft saved before requested');
    },
    async offline() {
      checkDraft();
      let failed = false;
      try { await bodyOnServer(); } catch (_) { failed = true; }
      assert(failed, 'server was not interrupted');
      save().click();
      await wait(() => document.querySelector('.editor-actions .error'), 'offline save error absent');
      checkDraft();
    },
    async recovered() {
      checkDraft();
      assert(await bodyOnServer() === original, 'offline save unexpectedly persisted');
      type(finalBody);
      await wait(() => editor()?.value === finalBody && !save().disabled, 'post-retry edit not accepted');
      save().click();
      await wait(() => save()?.disabled && !document.querySelector('.tab.active.dirty') &&
        document.querySelector('.editor-actions')?.textContent.includes('Saved.'), 'save did not finish cleanly');
      assert(await bodyOnServer() === finalBody, 'API body differs from full saved draft');
      await mode('Preview');
      await mode('Edit');
      assert(editor()?.value === finalBody, 'saved React state differs');
    },
    async cancelled() { checkDraft(); },
  };
  window.__wk3Run = stage => {
    window.__wk3Result = null;
    stages[stage]().then(() => { window.__wk3Result = { ok: true, stage }; })
      .catch(error => { window.__wk3Result = { ok: false, stage, error: String(error) }; });
  };
})();
