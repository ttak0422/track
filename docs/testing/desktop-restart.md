# Persistent desktop restart regression

Build the normal desktop app, then run on macOS 14 or newer:

```sh
make desktop-app
python3 scripts/desktop-restart-test.py build/desktop/Track.app
```

The runner copies the app into a fresh temporary fixture with a unique bundle identifier and a
fresh UUID-backed persistent `WKWebsiteDataStore`. The shipped bundle cannot opt into this mode.
The app checks that the store does not already exist before recording ownership. Restore and
cleanup require that ownership receipt. Normal launch still uses the default store; existing
smoke/recovery/shutdown tests retain their nonpersistent stores.

The seed app process opens two notes through the real router and selects Dark theme and Wide
content width using settings buttons. It exits through the normal supervised shutdown path.
A separate process loads the same fixed origin (`http://127.0.0.1:18765`) and owned store. Before
opening any note or changing any setting, it checks the restored tab order, labels and vault
qualification. It checks selected settings controls and applied DOM styles, then clicks each
restored tab and checks its route and rendered body. Both fixture vaults contain note `100`,
titled `Shared`, with different bodies; title-only assertions would miss cross-vault confusion.
No storage values are injected to simulate restoration.

Each app's server must exit and release its lease and ports before the next process starts.
Occupied ports cause an early refusal; the runner never kills an existing listener. On timeout,
only the fixture process group is terminated. A final headless cleanup process removes only the
owned UUID store through WebKit's API. The fixture directory is deleted only after that succeeds;
otherwise its path is reported and retained. The unique app identity separates app preferences
from the production Track app. The runner never clears the default WebKit store or opens a live
vault. Any system-managed caches/preferences for the disposable bundle identity are not swept.

This covers tab-strip persistence, two settings, and qualified-vault identity across an orderly
app-process restart. It does **not** establish full desktop persistence coverage: reading positions,
active-route restoration, all settings, crash/power-loss durability, duplicate launches, real port
collisions, or switching the launch vault between runs are not tested. The two fixture vaults
remain at the same paths and the launch vault stays alpha. Workflow/Makefile integration is
maintained separately.
