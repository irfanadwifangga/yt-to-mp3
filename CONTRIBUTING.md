# Contributing

Contributions to YouTube to MP3 & MP4 are welcome, including bug reports, documentation, translations, tests, and code. Please follow our [Code of Conduct](CODE_OF_CONDUCT.md).

## Before you start

- Search existing [issues](https://github.com/irfanadwifangga/yt-to-mp3/issues) and [pull requests](https://github.com/irfanadwifangga/yt-to-mp3/pulls) before opening a new one.
- For a substantial feature or architectural change, open an issue to discuss the approach before implementing it.
- Keep changes focused on one problem. Small fixes and documentation corrections can go directly to a pull request.
- Check the project scope and non-goals in the [design plan](docs/yt-to-mp3-go-planning.md).

## Reporting bugs and suggesting features

For a bug report, include the app version, operating system and architecture, steps to reproduce, expected behavior, and actual behavior. Add relevant logs or screenshots when useful, after removing personal information, session tokens, credentials, and private file paths.

For a feature request, describe the problem, who it affects, and the behavior you would like to see. Examples are helpful.

Do not disclose suspected security vulnerabilities in public issues. Use GitHub private vulnerability reporting if it is enabled for the repository; otherwise, arrange a private reporting channel with a maintainer before sharing sensitive details. Report conduct violations through the contact listed in the [Code of Conduct](CODE_OF_CONDUCT.md).

## Development setup

You will need:

- Git and Go 1.27 or newer, as specified in [go.mod](go.mod).
- Bun for local frontend commands and Node.js 24 or newer for the project's Node-based scripts.
- FFmpeg and ffprobe for integration tests, with `libmp3lame` and `libx264` available. yt-dlp is needed for manual conversion against YouTube.

Fork the repository on GitHub, clone your fork, and create a branch:

```sh
git clone https://github.com/YOUR-USERNAME/yt-to-mp3.git
cd yt-to-mp3
git switch -c docs/describe-your-change
```

Choose a branch name that describes your change. Install frontend dependencies and build the embedded frontend:

```sh
cd web
bun install --no-save
bun run build
cd ..
```

The first installation and Go build need network access to download dependencies. The frontend build produces `web/dist`, which is embedded in the Go application. Do not commit generated build output.

CI currently installs frontend dependencies from `web/package-lock.json` using npm. The local setup above does not save a Bun lockfile. Discuss dependency changes and their lockfile updates with a maintainer; avoid unrelated dependency or lockfile changes.

## Running locally

From the repository root, start the backend in one terminal:

```sh
go run ./cmd/app -dev -port 8799 -no-browser
```

In a second terminal, start the frontend:

```sh
cd web
bun run dev
```

Open the frontend on `http://localhost:5173` using the session token printed by the backend, as explained in the [development guide](docs/README.en.md#development). Keep that token private. Stop both processes with `Ctrl+C` when you finish.

## Checks before submitting

Run checks relevant to your change and report exactly what you ran. For code changes, the baseline checks are:

From the repository root:

```sh
gofmt -l ./cmd ./internal ./scripts ./web
go vet ./...
go test ./...
git diff --check
```

`gofmt -l` should print no file names. Format any Go files you changed before submitting.

From `web/`:

```sh
bun run typecheck
bun run test
bun run check:i18n
bun run build
```

For conversion or pipeline changes, also run integration tests from the repository root with FFmpeg and ffprobe available:

```sh
go test -count=1 -tags integration -run "Integrasi|E2E" ./internal/infrastructure/ffmpeg/ ./internal/e2e/
```

These tests use synthetic media and a fake yt-dlp for the end-to-end pipeline. They may skip when tools are unavailable; CI sets `YT2MP3_REQUIRE_TOOLS=1` to make missing tools a failure. Include any skips or unavailable tools in your pull request.

For documentation-only changes, check the wording, relative links, and `git diff --check`; application tests are usually unnecessary. CI also checks race conditions on Linux, tests on Windows and macOS, and builds the supported release targets. See [ci.yml](.github/workflows/ci.yml) for the current checks.

## Code and documentation conventions

- Follow the surrounding code and the dependency rules in [architecture.md](docs/architecture.md). Keep domain logic independent of HTTP and infrastructure implementations.
- Add or update tests for behavior changes. Prefer synthetic or controlled fixtures over public YouTube URLs.
- Keep user-facing translation keys and placeholders aligned in [English](web/src/locales/en.json) and [Indonesian](web/src/locales/id.json), and run `bun run check:i18n` from `web/`.
- Update both [English](docs/README.en.md) and [Indonesian](docs/README.id.md) documentation when behavior or setup instructions change. Keep each file in its named language. Mention in your pull request if you need help translating.
- Treat API golden files and database migrations as intentional contract changes. Explain these changes and their compatibility impact in your pull request.
- Never commit credentials, session tokens, personal application data, or downloaded media.

## Opening a pull request

1. Commit your focused changes and push the branch to your fork. Use a descriptive message; the project uses prefixes such as `docs:`, `fix:`, and `feat:`.
2. Open a pull request against `main`.
3. Describe the problem, the resulting behavior, related issues, and the checks you ran. Mention limitations, skipped tests, and any new dependencies with their purpose. Include screenshots for visible UI changes when useful.
4. Respond to review feedback and keep the pull request focused. Maintainers may request changes before merging.

Contributions are made under the project's [MIT license](LICENSE).
