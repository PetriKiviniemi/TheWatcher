# Tonttivahti

Watches the City of Oulu's building-plot service ([kartta.ouka.fi/eSite](https://kartta.ouka.fi/eSite/fi/SiteSearch))
and sends a push notification to your phone through [ntfy](https://ntfy.sh) when a plot matching your filters appears.

## Use

1. Run `TonttivahtiSetup.exe`. It installs for the current user (no admin rights) and opens the app in your browser.
2. Enter a username. There is no password: the name only keeps different people's alerts apart on this computer.
3. Pick plot types, keywords and price, rent or size limits, and how often to check (at least every 10 minutes).
4. Click **Generate** for a private ntfy topic, install the ntfy app on your phone, subscribe to that topic, and click
   **Save** and then **Send test alert**.

The app starts in the background when you log in to Windows. Reopen the page from the Start menu.
Saving a filter never sends alerts for plots that are already listed: you only hear about plots that start matching later.

- Settings: `%APPDATA%\Tonttivahti\profiles.json`. Log: `%APPDATA%\Tonttivahti\tonttivahti.log`.
- To stop it, click **Stop** in the top bar. It starts again at your next login.
- Uninstall from Windows Settings → Apps. Your settings file is kept.

## How it works

One Go program (`main.go`) serves the React UI from `web/` on `http://127.0.0.1:47321` and runs the watcher.

| File | Role |
| --- | --- |
| `plots.go` | Filter matching and "what is new since last check" |
| `watcher.go` | Polls `SiteSearchApi/SiteList`, sends ntfy alerts |
| `store.go` | Profiles and settings validation, saved as JSON |
| `server.go` | Local API: `GET`/`PUT /api/profiles/{name}`, `POST …/test`, `GET /api/unit-classes` |
| `web/src` | React + TypeScript + Base UI front end |

The city's API has no filtering of its own and returns every published plot (about 230), so filtering happens locally.
The plot list is cached for 2 minutes, so the city's service is never called more often than that.

The local server only answers requests addressed to `127.0.0.1` or `localhost` on its port, which blocks DNS-rebinding
attacks. Writes must come from the app's own origin with a JSON body, so other websites can't change your settings.
ntfy topics are public by name, so use the generated random one.

## Build

Needs Go 1.24+, Node 22+, `makensis` (NSIS) and, for signing, `osslsigncode`. The build works on Linux or macOS:

```bash
./build.sh
```

The output is `build/TonttivahtiSetup.exe` (installer) and `build/tonttivahti.exe`.
For development, run `npm --prefix web run build && go run .`. `go test ./...` runs the tests, and they need `web/dist` to exist.

## Code signing

Without a signature, Windows SmartScreen shows "Windows protected your PC" (click **More info → Run anyway**).
A signature needs a code-signing certificate, which nobody can make for you:

- **Certificate with an exportable `.pfx`:** `SIGN_PFX=cert.pfx SIGN_PASS_FILE=pass.txt ./build.sh` signs the app and
  the installer.
- **Hardware token or cloud signing** (most new OV/EV certificates, or Microsoft's Azure Artifact Signing): sign
  `build/tonttivahti.exe` before packaging and `build/TonttivahtiSetup.exe` after, using `signtool` on Windows or
  `jsign`/`osslsigncode -pkcs11module`.

Even when signed, SmartScreen may warn until the certificate has built up download reputation.
