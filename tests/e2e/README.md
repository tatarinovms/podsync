# Browser regression tests

Run from this directory with Node.js 20 or newer:

```bash
npm ci
npx playwright install chromium
npm test
```

On Linux, install browser system dependencies with
`npx playwright install --with-deps chromium` instead.

The test starts a temporary server on `127.0.0.1`, serves assets from
`services/web/ui/`, and mocks the Podsync API. It checks navigation, filters,
pagination, settings, subscriptions, polling, both themes, layouts from 280 to
951px, and a 300-feed library. It also checks axe-core accessibility rules,
combobox keyboard selection and cancellation, disabled controls, dynamic options,
required validation and form reset. It does not need a running Podsync instance
or credentials.
The UI's CDN scripts, styles, and icon fonts are served from pinned npm packages
by request interception. Once npm dependencies and Chromium are installed, the
test does not need internet access.

Screenshots are written to a temporary `podsync-ui-*` directory whose path is
printed by the test. Set `PLAYWRIGHT_CHROMIUM_EXECUTABLE` to use an existing
Chromium executable instead of the browser installed by Playwright.

CI runs this suite in the **Browser tests** job. The dependency-free UI tests
remain in `services/web/` and run in the regular Go test job.

Theme values live in `tokens/podsync.json`. From the repository root, regenerate
both UI copies with `python3 scripts/build_podsync_theme.py`, or check them with
`python3 scripts/build_podsync_theme.py --check`. The browser CI job runs this check.
