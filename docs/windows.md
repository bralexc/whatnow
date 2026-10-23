# Windows 11 (x64)

whatnow runs as one console executable with both the terminal interface and the embedded
browser interface. You do not need Go, Node, Python, Bash, a SQLite installation, or a separate
web server to run the downloaded executable. Use Windows Terminal with PowerShell for the TUI.

## Download and install

In the repository containing these changes, open **Actions → Windows x64**, select a successful
run, and download the **whatnow-windows-x64** artifact. You must be signed in to GitHub to download
workflow artifacts. Extract the ZIP and put `whatnow.exe` in a directory such as
`$env:LOCALAPPDATA\Programs\whatnow`. The artifact is retained for 30 days; run the workflow again
if it expires. This workflow uploads an artifact; it does not publish a GitHub release.

The workflow runs on pushes and pull requests and can also be started with **Run workflow**.
Keep it in your own fork or repository; no change or release in the original upstream is needed.

## Run

Open PowerShell in the directory containing the executable:

```powershell
.\whatnow.exe
```

This opens the terminal UI and serves the browser UI at `http://127.0.0.1:7777`.
Open that address in your browser to complete first-run model setup. Press `?` in the TUI for
keyboard shortcuts, and `q` or Ctrl+C to exit. The browser server stops when this process exits.

For the browser UI alone:

```powershell
.\whatnow.exe serve
```

Leave that terminal running and use Ctrl+C to stop it. To open the terminal companion while
the server is running, run `.\whatnow.exe tui` in another terminal. Use the same `-addr` and
`-db` options in both processes. Exiting the companion does not stop the separate server.
Avoid double-clicking the executable: launching it from a terminal keeps errors visible.

To use another port or data directory, quote paths that contain spaces:

```powershell
.\whatnow.exe serve -addr 127.0.0.1:7778 -db "$env:LOCALAPPDATA\whatnow data\whatnow.db"
```

The parent directory is created automatically. The default database is
`%APPDATA%\whatnow\whatnow.db`; `tui.log` lives alongside it. This location is independent of the
executable's directory and current working directory. SQLite may also create `-wal` and `-shm`
files alongside the database. Keep the database on a local disk, not a network share or a
directory synchronized while the app is running. Stop all whatnow processes before copying
the data directory for a backup or replacing the executable for an upgrade.

The listener defaults to loopback. Windows Firewall inbound rules are not needed for local use.
Existing remote-access settings and host checks still apply if you choose to expose it.

## Credentials and integrations

Model API keys and integration secrets use **Windows Credential Manager → Windows Credentials
→ Generic Credentials**, through the existing native keyring backend. They belong to the Windows
user running whatnow, with names such as `whatnow:anthropic`, `whatnow:openai`, and
`whatnow:integration/...`. They are not stored in SQLite or included in the executable.
The browser settings page manages them; use the same Windows account for the server and TUI.
Changing `-db` does not isolate credentials: databases used by the same Windows user share
the `whatnow` credential namespace.

`ANTHROPIC_API_KEY` and `OPENAI_API_KEY` are also supported as process environment variables when
no saved credential exists. Configure their values securely; do not put keys in build scripts or
GitHub Actions. Credential Manager takes precedence over the environment. No credentials are
needed for the build or automated tests, which use a fake model. Live model requests still need
your own provider key. The existing Google Calendar, Jira, and Review Queue integrations remain
available. IANA timezone data is embedded so calendar zones work without a Go installation.

If storing a secret fails, check that you are running in your normal signed-in Windows session.
The native backend limits individual secrets to 2,560 bytes; it reports an error rather than
falling back to a plaintext file.

## Build and test from source

Install Go 1.27+ and Node 24 x64, then open PowerShell in the repository root:

```powershell
$env:CGO_ENABLED = '0'
$env:GOARCH = 'amd64'
node scripts/build.mjs
.\dist\whatnow.exe version
.\dist\whatnow.exe
```

The build uses `npm ci`, builds Svelte, copies all generated assets into `internal/web/dist`
while preserving `.gitkeep`, and compiles the console executable into `dist\whatnow.exe`.
SQLite is pure Go; no C compiler or SQLite DLL is required. The binary is deliberately a console
application, not a `windowsgui` build, so both interfaces remain available.

Run the same checks as CI:

```powershell
go mod verify
go test ./...
go vet ./...
node scripts/smoke.mjs dist/whatnow.exe
Push-Location web
npm test
npm run check
npx --no-install playwright install chromium
npx --no-install playwright test
Pop-Location
```

The Windows-only tests exercise native credential save/read/update/delete using a unique test
namespace, default `%APPDATA%` handling, socket reuse, and standalone timezone loading. The
production smoke test runs outside the checkout, serves embedded JavaScript, writes a task to
a Unicode path containing spaces, and verifies persistence after restart. Existing TUI tests
exercise keyboard interactions and rendering. Playwright starts a separate temporary database
and an explicitly tagged fake-model executable; it never uses your production server or keys.

For frontend development, start the Go API and Vite together from the repository root:

```powershell
node scripts/dev.mjs
```

Use `http://localhost:5173` for Vite. Ctrl+C stops only the server and Vite children started by
the launcher. It also stops the other child if either process exits. Native scripts above do not
require `just` or Unix shell tools; the macOS release recipe is not part of the Windows build.
