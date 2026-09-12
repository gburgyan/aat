# Install

AAT is two binaries:

- `aat` — the CLI, the web UI, and the MCP server.
- `aat-sandbox` — the offline shop API behind the [shop example](examples/shop.md). You only need it to run that example.

| Method | Platforms | `aat` | `aat-sandbox` | Web UI |
|--------|-----------|-------|---------------|--------|
| [Release archive](#release-archives) | macOS, Linux, Windows (amd64, arm64) | yes | yes | yes |
| [Homebrew cask](#homebrew) | macOS, Linux | yes | yes | yes |
| [Docker image](#docker) | linux/amd64, linux/arm64 | yes | no | yes |
| [`go install`](#go-install) | wherever Go runs | yes | yes | no |
| [From source](#from-source) | wherever Go and Node.js run | yes | yes | yes |

The binaries need nothing at runtime beyond your project's YAML files; release binaries are statically linked (built with cgo disabled).

## Release Archives

Each release attaches one archive per platform. The names carry no version, so the download URL for the latest release never changes:

| OS | amd64 | arm64 |
|----|-------|-------|
| macOS | `aat_darwin_amd64.tar.gz` | `aat_darwin_arm64.tar.gz` |
| Linux | `aat_linux_amd64.tar.gz` | `aat_linux_arm64.tar.gz` |
| Windows | `aat_windows_amd64.zip` | `aat_windows_arm64.zip` |

Every archive holds `aat`, `aat-sandbox` (`aat.exe` and `aat-sandbox.exe` on Windows), `LICENSE`, and `README.md`. The release also carries `checksums.txt` with the SHA-256 of every archive.

The latest release is at `https://github.com/gburgyan/aat/releases/latest/download/<archive>`; a specific one is at `https://github.com/gburgyan/aat/releases/download/v0.1.0/<archive>`. On Linux:

```
curl -LO https://github.com/gburgyan/aat/releases/latest/download/aat_linux_amd64.tar.gz
curl -LO https://github.com/gburgyan/aat/releases/latest/download/checksums.txt
sha256sum --check --ignore-missing checksums.txt
tar -xzf aat_linux_amd64.tar.gz
sudo install aat aat-sandbox /usr/local/bin/
```

On macOS, check the download with `shasum -a 256 --check --ignore-missing checksums.txt` instead. The binaries are not notarized: if macOS refuses to run one you downloaded with a browser, remove the quarantine attribute with `xattr -d com.apple.quarantine aat aat-sandbox`.

To install the latest release in one step on macOS or Linux, without the checksum check:

```
curl -fsSL "https://github.com/gburgyan/aat/releases/latest/download/aat_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" \
  | sudo tar -xz -C /usr/local/bin aat aat-sandbox
```

On Windows, extract the zip and put the folder on your `PATH`. In PowerShell, for the amd64 build (open a new terminal afterwards):

```
$dir = "$env:LOCALAPPDATA\Programs\aat"
Invoke-WebRequest https://github.com/gburgyan/aat/releases/latest/download/aat_windows_amd64.zip -OutFile "$env:TEMP\aat.zip"
Expand-Archive "$env:TEMP\aat.zip" -DestinationPath $dir -Force
[Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ";$dir", 'User')
```

## Homebrew

```
brew install gburgyan/tap/aat
```

The cask installs both `aat` and `aat-sandbox`, on macOS and on Linux with Homebrew. On macOS it also removes the quarantine attribute from them, so the first run does not trip Gatekeeper.

## Docker

The image `ghcr.io/gburgyan/aat` contains `aat` only (no `aat-sandbox`) and is built for `linux/amd64` and `linux/arm64`. Each release is tagged with its version without the `v` (`ghcr.io/gburgyan/aat:0.1.0`), and a release that is not a prerelease also moves `latest`.

The image's entrypoint is `aat` and its working directory is `/work`, so mount your project there and pass the subcommand:

```
docker run --rm ghcr.io/gburgyan/aat:0.1.0 --version
docker run --rm -v "$PWD":/work ghcr.io/gburgyan/aat validate
docker run --rm -p 9119:9119 -v "$PWD":/work ghcr.io/gburgyan/aat web
docker run --rm -p 8080:8080 -v "$PWD":/work ghcr.io/gburgyan/aat mcp serve --http
```

`aat web` and `aat mcp serve --http` bind `127.0.0.1` by default, which a published port cannot reach inside a container, so the image sets `AAT_HOST=0.0.0.0`. Publishing the port with `-p` is then what exposes the server, and neither server has authentication. `-p 9119:9119` publishes on every interface of the host; use `-p 127.0.0.1:9119:9119` to keep it to your machine. See [Web UI](web-ui.md#starting-the-web-ui) and [SECURITY.md](https://github.com/gburgyan/aat/blob/main/SECURITY.md).

The container runs as a non-root user (UID 65532). Commands that write into the mounted project, such as `aat run` writing archives, need that user to have write access; if it does not, run the container as yourself with `--user "$(id -u):$(id -g)"`.

## `go install`

```
go install github.com/gburgyan/aat/cmd/aat@latest
go install github.com/gburgyan/aat/cmd/aat-sandbox@latest
```

This needs Go 1.25.7 or later (the `go` line in `go.mod`; a Go 1.21 or later toolchain downloads it automatically unless `GOTOOLCHAIN=local`) and puts the binaries in `$(go env GOPATH)/bin`, or in `GOBIN` when that is set.

A `go install` build has every CLI, MCP, and CI feature but no web UI: the frontend bundle is built with npm and is not part of the Go module. `aat web`, `aat web view`, and `aat web viewtrace` exit with code `2` and a hint:

```
aat: this build has no web UI (frontend bundle not embedded): install a release build from https://github.com/gburgyan/aat/releases or `brew install gburgyan/tap/aat`, or build from source with `make build`; the CLI, MCP server, and CI features work without it
```

The version comes from the Go build info, so `aat --version` prints the module version you installed.

## From Source

Requirements:

- Go 1.25.7 or later (see [`go install`](#go-install) for automatic toolchain downloads).
- Node.js 18, 20, or 22+ with npm, for the web UI (CI builds with Node.js 22).
- `git` and `make`. Without git history the version is `dev`.

```
git clone https://github.com/gburgyan/aat.git
cd aat
make build
```

| Target | Builds | Needs Node.js |
|--------|--------|---------------|
| `make build` | The frontend (`npm install && npm run build` in `server/web`), then `./aat` and `./aat-sandbox` | yes |
| `make cli` | `./aat` only, embedding whatever `server/web/dist/app` already holds | no |
| `make sandbox` | `./aat-sandbox` only | no |

All three inject the version (`git describe`), commit, and build date. `make cli` from a fresh clone has no frontend bundle, so `aat web` exits with code `2` as described under [`go install`](#go-install); run `make build` once and later `make cli` builds keep the UI.

## Verifying the Install

```
aat --version
aat-sandbox --version
```

Each prints `<name> version <version> (commit: <commit>, built: <date>)`. A release build prints the version without the `v` (`0.1.0`) with its short commit and UTC build time; a `go install` build prints `v0.1.0` with `commit: unknown, built: unknown`; a `make` build prints the `git describe` output (a tag, or a tag plus the commits since it, such as `v0.1.0-3-g1a2b3c4`).

To try the whole toolchain offline, extract the shop example and follow its README:

```
aat-sandbox init shop && cd shop
```

See [Examples: Shop](examples/shop.md), or start with the [Quickstart](quickstart.md) or the [Tutorial](tutorial.md).

---

*Source: `.goreleaser.yml`, `Dockerfile`, `Makefile`, `go.mod`, `server/web/package.json`, `internal/version/version.go`, `cmd/aat/web_cmd.go`, `server/embed.go`.*
