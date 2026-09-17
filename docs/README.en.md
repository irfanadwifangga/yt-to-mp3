# Youtube To MP3 Converter

**English** · [Bahasa Indonesia](README.id.md)

A local desktop app that converts YouTube audio to MP3 (technical name: `yt-to-mp3`, used for the repository, executable, and data directory): a single Go binary that runs a loopback server and serves an embedded React SPA. There is no cloud service; all work and data stay on your machine.

> **Status: core features and hardening are done.** Analysis, queue, conversion, history, settings, one-click tool installation, logging, housekeeping, and idle shutdown all work, and the measured NFR targets pass. The release pipeline is ready, but no release has been published yet and binaries are not signed. Read [Known limitations](#known-limitations) before use.

## Features

- **Analyze without downloading** — paste a link and a preview of the title, channel, duration, and source codec appears right away.
- **Live progress** per job over SSE, shown as the video cover filling with color from the bottom up. Cancelling a job stops the whole process tree, including the FFmpeg that yt-dlp runs, then cleans up temporary files.
- **Finished list** — open the output folder in your file manager, save a copy, retry failed jobs, or remove them from the list (with or without the file). Long histories load incrementally.
- **Auto-retry** — transient network failures are retried automatically up to 3 times with growing delays. When YouTube rate-limits requests (HTTP 429), the delay is longer and parallel jobs drop to one for a few minutes.
- **Five MP3 presets**: 128, 192, 256, and 320 kbps CBR, plus VBR V0. 48 kHz stereo output with ID3v2.3 tags and an embedded square cover.
- **Edit title and artist before converting**, prefilled with cleaned-up suggestions (without `(Official Video)` and the like). Both are used for tags and the file name. For videos linked to YouTube Music, suggestions come from the catalog, and album and release year tags are written as well.
- **Duplicate conversion warning** when the same video was already converted with the same preset.
- **Settings dialog** (the **Settings** button or `Ctrl+,`), with the output folder picked through the operating system's native folder dialog.
- **Indonesian and English UI**, with a light, dark, or system theme.
- **Its own window on Windows** — the UI opens as a standalone Microsoft Edge app window without tabs or an address bar. Other systems use the default browser.
- **One binary, one instance** — launching it again opens the running instance instead of a second server.

## Installation

**Windows:** download `yt-to-mp3_<version>_windows_amd64_setup.exe` from the Releases page and run it. No admin rights are needed. The installer creates a Start Menu shortcut, and the app can be uninstalled from **Settings → Apps**. Uninstalling keeps your history and converted MP3 files.

The installer and executable are not signed yet, so SmartScreen may show "Windows protected your PC". Choose **More info**, then **Run anyway**. Compare the file with the `.sha256` on the release page if you want to be sure the download is intact.

The app opens in its own window. Closing the window quits the app once no conversion is running; conversions still in progress are finished first. The **Quit** button in the top right quits right away. On first launch, install yt-dlp and FFmpeg from **Settings → Tools**. If Microsoft Edge is not available, the UI opens in your default browser instead.

**macOS and Linux:** download the `tar.gz` archive for your architecture, extract it, and run `yt-to-mp3`. On macOS, run `xattr -d com.apple.quarantine ./yt-to-mp3` first because the binary is not signed. The UI opens in your default browser.

## Requirements

| Tool | Version | Used for |
| --- | --- | --- |
| Go | 1.27+ | Building the backend |
| Node.js | 24+ | Building the SPA; not needed at runtime |
| yt-dlp | latest | Reading metadata and downloading audio |
| FFmpeg and ffprobe | with `libmp3lame`; tested on 9.0.1 | Converting and verifying the output |

The first build downloads Go modules and npm packages, so it needs network access once. Every Go dependency is pure Go (no cgo), so cross-compiling needs no C toolchain.

### Installing yt-dlp and FFmpeg

The easiest way: open **Settings → Tools** and press **Install**. The app downloads the versions pinned in its manifest, verifies SHA-256 before extracting, and installs them into the app data directory. yt-dlp comes from its official releases, and FFmpeg from GyanD (Windows) and martin-riedl.de (Linux, macOS); the reasoning is in [ADR-031](yt-to-mp3-go-planning.md#4-keputusan-teknis).

Tools installed with a package manager work too; the app finds them on `PATH`.

Windows:

```bash
winget install yt-dlp.yt-dlp
```

```bash
winget install Gyan.FFmpeg
```

macOS:

```bash
brew install yt-dlp ffmpeg
```

Linux: install FFmpeg from your distribution's packages. For yt-dlp, use the binary from its [official releases](https://github.com/yt-dlp/yt-dlp/releases) — distribution packages often lag behind, while yt-dlp has to keep up with changes on YouTube's side.

Tools installed into a directory that is already on `PATH` are detected within 30 seconds without a restart. If the installation adds a new directory to `PATH` — usually the first winget install — reopen the app, because a running process does not see `PATH` changes.

## Quick start

```bash
make install-web && make build && make run
```

Without `make`:

```bash
npm --prefix web install && npm --prefix web run build && go build -o bin/yt-to-mp3 ./cmd/app && ./bin/yt-to-mp3
```

The app picks a random port on `127.0.0.1` and opens the UI. The launch URL carries a one-time session token, which is moved to `sessionStorage` immediately and removed from the address bar.

How to use it:

1. Paste or type a YouTube video URL. Analysis starts automatically once the link is complete; there is no button to press (Enter still works).
2. Pick a preset, then press **Convert**.
3. Follow the progress under **In progress**. When it finishes, a notification tells you the file is saved in the output folder (shown at the bottom of the page), with a **Show in folder** button. There is no need to download the file again; **Save a copy** in the **Finished** list only makes a duplicate through the browser.

To stop the app, press **Quit** in the top right, close the app window, or press `Ctrl+C` in the terminal. When the UI is in a browser tab, the app also stops on its own after the tab is closed and no job has run for 30 minutes; the limit can be changed or disabled (0) in **Settings → Advanced**. There is no tray icon because tray libraries need cgo, which would break cross-compilation ([ADR-021](yt-to-mp3-go-planning.md#4-keputusan-teknis)).

## Configuration

Settings can be changed from the **Settings** dialog, through `config.json` in the data directory (created on first run), or through environment variables:

| Variable | Equivalent |
| --- | --- |
| `YT2MP3_OUTPUT_DIR` | `output_dir` |
| `YT2MP3_LOG_LEVEL` | `log_level` |
| `YT2MP3_MAX_CONCURRENT_JOBS` | `max_concurrent_jobs` |

Precedence, from lowest to highest: built-in defaults, `config.json`, environment variables, then changes made in the UI.

Almost every setting applies as soon as it is saved from the UI, including the output folder and log level. The exception is **parallel jobs**, which applies after the app is reopened because the worker capacity is set up at startup.

The output folder is picked through the operating system's native folder dialog. The local server opens that dialog, not the browser, because browsers never tell a web page the absolute path of a folder. On Linux the dialog needs `zenity`, `kdialog`, or `qarma`; without one of them, type the folder path by hand. A new folder is checked for write access before it is saved, and conversions already running finish in the previous folder.

## Development

Run the backend and frontend separately so Vite hot reload keeps working. In two terminals:

```bash
make dev-api
```

```bash
make dev-web
```

In dev mode the backend uses the fixed port 8799 (production stays random) because the Vite proxy needs a predictable target. The `-dev` flag allows the `localhost:5173` origin and is never enabled in release builds.

The session token can only arrive through the URL, so open the SPA with the token from the `make dev-api` terminal output (the `buka:` line). That tokenized URL is deliberately printed only to the terminal and never to the log file:

```
http://localhost:5173/?token=<token-from-output>
```

### Checks

| Command | What it does |
| --- | --- |
| `make check` | Formatting, `go vet`, and every Go test |
| `make typecheck` | Frontend type checking |
| `make test-web` | Frontend component tests with vitest and jsdom: auto-analysis, stale analysis results, tag editing, duplicate warnings, yt-dlp updates, notifications, and history merging |
| `make check-i18n` | Every Go error code has `id` and `en` translations, and both locales have the same keys |
| `make test-integration` | Conversion with real FFmpeg and ffprobe using synthetic fixtures (a sine tone and a plain cover), plus end-to-end job pipeline tests with a fake yt-dlp: conversion, auto-retry, and cancelling while downloading. Fails when ffmpeg is missing |
| `make nfr` | Measures the [NFR](yt-to-mp3-go-planning.md#22-non-functional-requirements) targets in a temporary data directory; `URL="<link>"` also measures two parallel conversions |

API response shapes are locked by golden files in `internal/api/testdata/golden/` and checked by `make test`. Intentional contract changes are rewritten with `go test ./internal/api/ -run Kontrak -update`, and the diff is reviewed before committing.

CI runs every check above except `make nfr`, plus Go tests on Windows and macOS (process tree termination and atomic rename differ per OS). The **Tool regression** workflow installs yt-dlp and FFmpeg exactly as pinned in the manifest on all five release targets and converts a fixture with those binaries. It runs whenever tool code or the manifest changes, and weekly to catch upstream URLs that break. `make help` lists every target.

Some tests run real processes — the process tree termination test spawns a child and a grandchild to prove both die — so the `process` package takes a few seconds.

### Releasing

GoReleaser builds releases from `v*` tags through the **Release** workflow: archives for windows/amd64, linux/amd64, linux/arm64, darwin/amd64, and darwin/arm64 named `yt-to-mp3_<version>_<os>_<arch>`, plus `checksums.txt`. The release is created as a **draft** and only becomes public after it is published by hand from the Releases page.

Before the first tag, validate the pipeline by running the **Release** workflow manually (snapshot mode, no release is created). Its artifacts are named after the files inside them, such as `yt-to-mp3_<version>_windows_amd64_setup`. You can also run a snapshot locally when goreleaser v2 is installed:

```bash
make release-snapshot
```

yt-dlp and FFmpeg are never included in release archives; users install them from the app ([ADR-031](yt-to-mp3-go-planning.md#4-keputusan-teknis)). Signing is not set up yet: macOS needs codesign and notarization with an Apple Developer account, and Windows needs a code signing certificate.

The Windows build differs from the other targets ([planning §25](yt-to-mp3-go-planning.md#25-build-dev-workflow-dan-rilis)):
- It is linked as a GUI application without a console window. Startup errors appear as a dialog.
- It carries an icon, version info, and a manifest.
- The **Windows installer** job wraps it into `setup.exe` with Inno Setup.

For a local build with the icon and version info, run `make winres` before `go build`. `make icon` redraws the icons from the brand mark.

## Architecture overview

![Architecture overview of Youtube To MP3 Converter: backend, embedded UI, local state and platform, and conversion tools](diagram-ytmp3.png)

The diagram shows how the pieces fit together at runtime:

- **Backend** — `cmd/app/main.go` assembles everything: the single instance guard, the local HTTP API with its live SSE job stream, the job scheduler, and the conversion pipeline. `internal/adapters` only bridges infrastructure types to the application ports.
- **Embedded UI** — the React app is embedded into the binary with `go:embed` and served by the same local server. It calls the API through `api.ts` and follows job progress over SSE through `useJobStream.ts`.
- **Local state and platform** — SQLite stores jobs, history, and settings, with its schema evolved by embedded migrations. The filesystem store writes MP3 output through atomic commits. The app window (`internal/browser`), native dialogs (`internal/dialog`), and "Show in folder" talk to the operating system.
- **Conversion tools** — the pipeline runs yt-dlp to download audio and FFmpeg to probe and transcode, both as local subprocesses. Managed copies of both tools are installed and updated from inside the app.

Settings, housekeeping, idle shutdown, and update checks are left out of the diagram for readability. [architecture.md](architecture.md) (in Indonesian) covers every component.

## Project structure

```text
cmd/app/              entry point and dependency wiring
internal/
  adapters/           bridges infrastructure to application ports, shared with E2E tests
  api/                HTTP handlers, security middleware, SSE hub
  application/        use cases, ports, pipeline orchestration
  domain/             entities, state machine, error taxonomy
  infrastructure/
    db/               SQLite, repositories, migration runner
    ffmpeg/           transcoding, progress parsing, verification with ffprobe
    fs/               name reservation, atomic commit, name sanitizing, disk space
    process/          child processes and per-OS process tree termination
    tools/            discovering and installing yt-dlp and FFmpeg
    ytdlp/            metadata, downloads, error mapping
  worker/             scheduler and worker pool
  config/             configuration and per-OS storage locations
  instance/           single instance guard
  browser/            default browser, Edge app window, and file manager
  dialog/             native dialogs
  version/            build identity
web/                  React + TypeScript SPA, embedded with go:embed
  src/locales/        id and en translation dictionaries
  scripts/            translation completeness checker
migrations/           SQL migrations, embedded
packaging/windows/    icon, manifest, version info, Inno Setup script
scripts/              tool manifest updater and icon generator
docs/                 planning, architecture, data model (in Indonesian)
```

Imports flow one way: `api` and `infrastructure` depend on `application`, `application` depends only on `domain`, and `domain` depends on nothing. Details are in [architecture.md](architecture.md).

## API

Every endpoint lives under `/api` and requires the `X-Session-Token` header, except `GET /api/ping`, which is used to detect a running instance. The full contract is in [planning §7](yt-to-mp3-go-planning.md#7-kontrak-api).

| Endpoint | Purpose |
| --- | --- |
| `GET /health` | App, tool, and queue status |
| `GET /tools` · `POST /tools/install` | Tool status and installation |
| `POST /tools/check` · `POST /tools/update` | Check for newer versions; update yt-dlp to the latest release |
| `GET /presets` | List presets |
| `POST /metadata` | Analyze a URL without downloading |
| `POST /jobs` · `GET /jobs` · `GET /jobs/{id}` | Create, list, and read jobs |
| `GET /jobs/{id}/events` | Live progress (SSE, supports `Last-Event-ID`) |
| `POST /jobs/{id}/cancel` · `POST /jobs/{id}/retry` · `DELETE /jobs/{id}` | Cancel, retry, delete (`?delete_file=true` also deletes the output file) |
| `GET /files/{id}` · `POST /files/{id}/reveal` | Download an output file, show its location |
| `GET /settings` · `PUT /settings` | Read and change settings |
| `POST /dialogs/folder` | Open the native folder picker |
| `POST /shutdown` | Stop the app |

## Data locations

| OS | Data | Default output |
| --- | --- | --- |
| Windows | `%LOCALAPPDATA%\yt-to-mp3\` | `%USERPROFILE%\Music\yt-to-mp3\` |
| macOS | `~/Library/Application Support/yt-to-mp3/` | `~/Music/yt-to-mp3/` |
| Linux | `$XDG_DATA_HOME/yt-to-mp3/` | `$XDG_MUSIC_DIR` or `~/Music/yt-to-mp3/` |

The data directory holds the SQLite database (`db/app.db`), `config.json`, tools installed by the app, temporary files, logs, the Edge profile used by the app window on Windows (`window/`), and `runtime.json` while the app is running.

Logs are written to `logs/app.log` and to the terminal, rotated daily into `logs/app-YYYY-MM-DD.log`, and archives older than 7 days are deleted. Attach this file when reporting a bug; the session token is never written there.

The app maintains its own data at startup and then every hour: event trails of jobs finished more than 30 days ago are pruned (the jobs and their files stay), expired metadata cache entries are removed, orphaned temporary files are cleaned up, and output file status is reconciled with the disk. Files deleted outside the app are marked missing and become available again if they reappear, for example after reconnecting an external drive.

## Security

A local server is not a private server: any website the user has open can send requests to `127.0.0.1`. That is why this server binds explicitly to loopback, allowlists the `Host` header against DNS rebinding, checks `Origin` on every state-changing request, uses a random per-process session token, denies all CORS, and limits body size. File paths are never accepted from the client, and tool arguments never go through a shell. Details are in [planning §15](yt-to-mp3-go-planning.md#15-model-keamanan-lokal).

## Known limitations

- **FFmpeg installed by the app only moves forward with app releases.** yt-dlp can be updated directly from **Settings → Tools** (verified against the `SHA2-256SUMS` of its official release), but FFmpeg follows the pinned manifest; to move it forward, run `make update-tools` and commit.
- **The binaries and installer are not signed**, so SmartScreen on Windows and Gatekeeper on macOS warn on first launch.
- **The app only tells you when a new version is available**; it does not update itself, so download the new installer from the Releases page. The check reads GitHub releases of `irfanadwifangga/yt-to-mp3`, so it only works once releases are published in a public repository.
- **The Windows build has no console, so logs are only available in** `logs/app.log` in the data directory. To see logs live, run from source with `go run ./cmd/app`.
- **The Windows app window is Microsoft Edge running with a separate profile**, not a native window. If Edge is missing, the UI falls back to the default browser, and closing that tab no longer quits the app right away; idle shutdown ends it later.

Deliberately unsupported: playlists, live streams, videos that require sign-in or are age-restricted, and video output. The full list is in the [non-goals](yt-to-mp3-go-planning.md#3-non-goals).

## Documentation

The design documents are written in Indonesian.

| Document | Contents |
| --- | --- |
| [yt-to-mp3-go-planning.md](yt-to-mp3-go-planning.md) | Scope, 34 ADRs, API contract, roadmap, acceptance criteria |
| [architecture.md](architecture.md) | Dependency rules, ports and adapters, concurrency model, lifecycle |
| [data-model.md](data-model.md) | SQLite schema, invariants, retention, migrations |

## Legal note

Downloading YouTube content generally violates that platform's Terms of Service, and copyright in the downloaded material stays with its owners. This project is meant as a local, personal tool and does not provide distribution, sharing, or hosting of content. As a practical consequence, yt-dlp breaks periodically as YouTube changes, which is why the tools are deliberately kept separate from the app's release cycle.

## License

[MIT](../LICENSE)
