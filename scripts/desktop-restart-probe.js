// Executed only inside the disposable fixture's WKWebView. No storage values are seeded directly.
(() => {
  window.__trackWebSmoke = {done: false};
  (async () => {
    const pause = () => new Promise(resolve => setTimeout(resolve, 100));
    const check = (value, message) => { if (!value) throw new Error(message); };
    const wait = async (predicate, message) => {
      const deadline = Date.now() + 10000;
      while (Date.now() < deadline) {
        if (predicate()) return;
        await pause();
      }
      throw new Error(message);
    };
    const buttons = () => [...document.querySelectorAll('.tab-label')];
    const labels = () => buttons().map(b => ({
      vault: b.querySelector('.tab-vault')?.textContent || '',
      title: b.querySelector('.tab-title')?.textContent
    }));
    const reader = () => document.querySelector('.markdown-view')?.textContent || '';
    const settings = () => document.querySelector('button[aria-label="Settings"]');
    await wait(() => settings(), 'workspace did not render');
    const response = await fetch('/api/vaults');
    const vaults = await response.json();
    check(response.ok && vaults.active?.path === FIXTURE_ROOT + '/alpha', 'wrong active vault');
    check(location.origin === 'http://127.0.0.1:18765', 'origin changed');
    if (PHASE === 'seed') {
      check(JSON.parse(localStorage.getItem('track.tabs') || '[]').length === 0, 'fixture tabs were not fresh');
      // Route through the real router and let its normal tab effects persist the state.
      for (const [id, body] of [['100', 'ALPHA_FIXTURE_BODY'], ['beta~100', 'BETA_FIXTURE_BODY']]) {
        history.pushState(null, '', '/notes/' + id);
        dispatchEvent(new PopStateEvent('popstate'));
        await wait(() => reader().includes(body), 'note UI did not render ' + id);
      }
      settings().click();
      await wait(() => document.querySelector('[role="group"][aria-label="Theme"]'), 'settings not open');
      for (const [group, text] of [['Theme', 'Dark'], ['Content width', 'Wide']]) {
        const button = [...document.querySelectorAll(`[role="group"][aria-label="${group}"] button`)]
          .find(b => b.textContent === text);
        check(button, 'missing setting ' + text);
        button.click();
      }
    } else {
      // Assert the restored strip before any navigation or setting changes can repopulate it.
      await wait(() => buttons().length === 2, 'two tabs were not restored after process restart');
      check(JSON.stringify(labels()) === JSON.stringify([
        {vault: 'beta', title: 'Shared'}, {vault: '', title: 'Shared'}
      ]), 'restored tab order, titles, or vault identity changed: ' + JSON.stringify(labels()));
      settings().click();
      await wait(() => document.querySelector('[role="group"][aria-label="Theme"]'), 'settings not open');
    }
    await wait(() => document.documentElement.dataset.theme === 'dark' &&
      document.documentElement.style.getPropertyValue('--content-width') === '1280px', 'settings were not applied');
    for (const [group, text] of [['Theme', 'Dark'], ['Content width', 'Wide']]) {
      check([...document.querySelectorAll(`[role="group"][aria-label="${group}"] button`)]
        .some(b => b.textContent === text && b.getAttribute('aria-pressed') === 'true'), 'setting control not restored: ' + text);
    }
    settings().click();
    await wait(() => buttons().length === 2, 'two tabs missing');
    const restoredTabs = labels();
    // Same IDs and titles in both vaults: the body and route must follow the clicked tab's vault.
    for (const [vault, id, body, other] of [
      ['', '100', 'ALPHA_FIXTURE_BODY', 'BETA_FIXTURE_BODY'],
      ['beta', 'beta~100', 'BETA_FIXTURE_BODY', 'ALPHA_FIXTURE_BODY']
    ]) {
      const button = buttons().find(b => (b.querySelector('.tab-vault')?.textContent || '') === vault);
      check(button, 'missing tab for vault ' + vault);
      button.click();
      await wait(() => reader().includes(body), 'clicked restored tab displayed wrong vault');
      check(!reader().includes(other) && decodeURIComponent(location.pathname) === '/notes/' + id,
        'cross-vault confusion');
    }
    // Allow asynchronous WebKit disk persistence; the next process is the actual durability check.
    await new Promise(resolve => setTimeout(resolve, 1500));
    window.__trackWebSmoke = {done: true, ok: true, phase: PHASE, restoredTabs,
      theme: document.documentElement.dataset.theme,
      contentWidth: document.documentElement.style.getPropertyValue('--content-width'),
      origin: location.origin, activePath: vaults.active.path};
  })().catch(error => {
    window.__trackWebSmoke = {done: true, ok: false, error: String(error),
      text: document.body.innerText.slice(0, 1500), path: location.pathname};
  });
  return true;
})()
