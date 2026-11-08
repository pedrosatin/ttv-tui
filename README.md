# ttv-tui

[![CI](https://github.com/pedrosatin/ttv-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/pedrosatin/ttv-tui/actions/workflows/ci.yml)

[Português (Brasil)](docs/README.pt-BR.md)

Terminal UI for Twitch on Linux, macOS and Windows. Browse live categories and channels, watch streams through `streamlink` and `mpv` with ad filtering and in-app playback controls, and participate in IRC chat without opening a browser.


## v1.8 support

| OS / architecture | Support |
| --- | --- |
| Linux amd64 / arm64 | Supported |
| macOS arm64 (Apple Silicon) | Supported |
| Windows x64 (amd64) | Supported |
| macOS Intel (darwin/amd64) | Experimental; builds without a support commitment |
| Windows ARM (windows/arm64) | Experimental; builds without a support commitment |

The matrix defines v1.9 targets. Cross-compilation does not verify playback, audio, login, clipboard or shutdown on real hardware. Release v0.4.1 includes archives for all three operating systems.

Browsing, search, local favorites, IRC chat and external video with pause/volume form the scope on supported targets. Use a terminal with Unicode and ANSI colors, at least 80 columns wide for split view; chat-only works below that width. Linux video requires a configured graphical session and audio. On Windows, use Windows Terminal; automated ConPTY tests are outside this version.

## Install

### Linux and macOS: install script

[`packaging/install.sh`](packaging/install.sh) downloads the release archive and `checksums.txt`, verifies the SHA-256 digest with `sha256sum` or `shasum -a 256`, and installs the binary in `~/.local/bin/ttv-tui`, the desktop entry in `~/.local/share/applications/ttv-tui.desktop`, and the icon in `~/.local/share/icons/hicolor/scalable/apps/ttv-tui.svg`:

```bash
# Install the latest release (or pass a specific version such as 0.4.1)
./packaging/install.sh
./packaging/install.sh 0.4.1

# Preview the installation plan without downloading or modifying files
./packaging/install.sh --dry-run 0.4.1

# Install into a custom prefix
./packaging/install.sh --prefix "$HOME/.local" 0.4.1
```

You can also run the script directly from GitHub:

```bash
curl -fsSL https://raw.githubusercontent.com/pedrosatin/ttv-tui/main/packaging/install.sh | bash -s -- 0.4.1
```

On macOS, the script installs only the binary in `~/.local/bin`; the desktop entry and icon are Linux-only. Use the `darwin_arm64` archive for Apple Silicon. Browser-downloaded binaries without notarization may need approval in System Settings → Privacy & Security (Gatekeeper), after checking the source and checksum. Paid notarization is outside scope. Script/Homebrew downloads do not apply the browser's quarantine attribute.

### Windows x64: PowerShell

From a checkout, install version 0.4.1:

```powershell
.\packaging\install.ps1 -DryRun
.\packaging\install.ps1 -Version 0.4.1
winget install --id shinchiro.mpv -e
winget install --id streamlink.streamlink -e
```

The script verifies SHA-256, installs in `%LOCALAPPDATA%\Programs\ttv-tui\bin` and adds that directory to the user PATH. Open a new terminal.

### Homebrew (macOS) and Scoop (Windows)

The [Homebrew formula](packaging/homebrew/README.md) uses the v0.4.1 macOS release for Apple Silicon and Intel. It has not been published or validated on macOS. After publishing the tap, the intended command is `brew install pedrosatin/tap/ttv-tui`. Dependencies can be installed with `brew install mpv streamlink`.

The [Scoop manifest](packaging/scoop/ttv-tui.json) uses the v0.4.1 Windows amd64 release and its SHA-256. Dependencies use `scoop bucket add extras` and `scoop install extras/mpv extras/streamlink`.

### Release binary (v0.4.1)

Download the archive for your OS and architecture and `checksums.txt` from the [v0.4.1 release](https://github.com/pedrosatin/ttv-tui/releases/tag/v0.4.1). Examples are `ttv-tui_0.4.1_linux_amd64.tar.gz`, `ttv-tui_0.4.1_darwin_arm64.tar.gz` and `ttv-tui_0.4.1_windows_amd64.zip`. Linux and macOS also have the other architecture in tar.gz; Windows arm64 is available in zip. See the support matrix above.

Verify the archive's SHA-256 against its entry in `checksums.txt` before extracting it. On Linux use `sha256sum`, on macOS `shasum -a 256`, or on Windows PowerShell `Get-FileHash -Algorithm SHA256`. Extract `ttv-tui` (or `ttv-tui.exe` on Windows) and place it in a directory on your `PATH`.

### Go install

Requires Go 1.27 or newer:

```bash
go install github.com/pedrosatin/ttv-tui/cmd/ttv-tui@v0.4.1
```

### Arch Linux (AUR)

An AUR package (`ttv-tui-bin`) is planned and not published yet. Until it is available on the AUR, you can build and install it directly from [`packaging/PKGBUILD`](packaging/PKGBUILD):

```bash
cd packaging
makepkg -si
```

### Build from source

Requires Go 1.27:

```bash
git clone https://github.com/pedrosatin/ttv-tui.git
cd ttv-tui
CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=0.4.1" -o ~/.local/bin/ttv-tui ./cmd/ttv-tui
```

### Desktop shortcut

[`packaging/install.sh`](packaging/install.sh) and [`packaging/PKGBUILD`](packaging/PKGBUILD) install [`packaging/ttv-tui.desktop`](packaging/ttv-tui.desktop) and [`assets/ttv-tui.svg`](assets/ttv-tui.svg) automatically. If you installed the binary with `go install` or `go build`, copy them manually:

```bash
install -Dm644 packaging/ttv-tui.desktop ~/.local/share/applications/ttv-tui.desktop
install -Dm644 assets/ttv-tui.svg ~/.local/share/icons/hicolor/scalable/apps/ttv-tui.svg
update-desktop-database ~/.local/share/applications
```

The desktop entry runs `xdg-terminal-exec ttv-tui` with `Terminal=false` and `StartupWMClass=ttv-tui`. If `xdg-terminal-exec` is not installed on your system, edit `~/.local/share/applications/ttv-tui.desktop` and either set `Terminal=true` with `Exec=ttv-tui` or call your terminal emulator directly, such as `Exec=alacritty --class ttv-tui -e ttv-tui` or `Exec=kitty --app-id ttv-tui ttv-tui`.

## Requirements

### Required (video playback)

- An OS from the support matrix and a Unicode/ANSI terminal.
- `streamlink` in `PATH` to negotiate stream playback and filter ad segments. Without it, video playback is unavailable.
- `mpv` in `PATH` to decode and display video in its own window while exposing a local JSON IPC socket. Without it, video playback is unavailable.

### Optional (login and chat)

- `sqlite3` in `PATH`. `ttv-tui` runs the `sqlite3` command-line binary only during `ttv-tui login --from-browser` to copy and query your browser's cookie database via SQLite's online `.backup` command without contesting an open browser lock. Not needed if you browse and watch anonymously or import tokens via `ttv-tui login --import twt`.
- A supported browser logged into `twitch.tv` (Brave, Chrome, Chromium, Edge, Vivaldi, Opera, or Firefox) if you want to send chat messages via `--from-browser`. On Linux Chromium-family browsers, `secret-tool` (`libsecret`) must also be in `PATH` to read the OSCrypt Safe Storage secret from the desktop keyring. Browsing categories, watching streams, and reading chat work anonymously without logging in.

## Quick start

1. Configure a Twitch token if you want to send chat messages. When you already have `twitch-tui` (`twt`) configured at `~/.config/twt/config.toml` or want a chat-scoped token without reading browser cookies or the desktop keyring, prefer importing from `twt`:

```bash
ttv-tui login --import twt
```

Otherwise, extract the `auth-token` session cookie from an installed browser. Read [Login and security](#login-and-security) for the reach of the keyring secret and session cookie:

```bash
ttv-tui login --from-browser
```

If you have multiple browser profiles, pass `--browser` and `--profile` directly or pick from the interactive prompt:

```bash
ttv-tui login --from-browser --browser brave --profile Default
```

2. Launch the interface:

```bash
ttv-tui
```

The UI language follows `--lang` (`en` or `pt-BR`), the `language` setting in `config.toml`, or your `LC_ALL` / `LC_MESSAGES` / `LANG` environment variables, defaulting to English.

## Walkthrough

When `ttv-tui` starts, it loads the top live categories on Twitch.


Select a category with `Enter` to list its live channels, then press `Enter` on a channel to launch video playback in `mpv` and join the channel's IRC chat in the right panel.


Press `?` or `F1` at any time to display the in-app keyboard shortcuts overlay.


For a step-by-step tour covering installation, authentication, channel switching, favorites, chat, narrow terminals, and shutdown, read the [full walkthrough](docs/walkthrough.md).

## Keybindings

Press `?` or `F1` to open the modal help. It captures keys; use `↑`/`↓`, `PgUp`/`PgDn`, or `Home`/`End` to scroll and `?`, `Esc`, `F1`, or `q` to close it. The tables below list all bindings, including two details omitted from the compact overlay: the unshifted `=` and `_` volume aliases, and `F1` working while typing in a prompt.

### List navigation and search

| Key | Action |
| --- | --- |
| `↑` / `↓` or `j` / `k` | Move selection up or down in the list, loading more items at the bottom |
| `Enter` | Open the highlighted category or start watching the highlighted channel |
| `f` | Toggle the highlighted channel in Favorites |
| `/` | Filter the current list locally (`Enter` keeps filter, `Esc` clears) |
| `Ctrl+F` | Search channels across Twitch, reusing active `/` filter text if present |
| `Ctrl+S` | Same as `Ctrl+F` (alias). Works in any terminal: `ttv-tui` runs in raw mode, which disables the terminal's software flow control |
| `r` | Retry loading the current list or retry a failed video playback |
| `Esc` | Clear active filter, or step back from channels / search results to categories |
| `Ctrl+R` | Return to Live Categories from anywhere, even while typing |
| `Tab` | Switch keyboard focus between the list panel and the chat panel |

### Chat

| Key | Action |
| --- | --- |
| `↑` / `k`, `↓` / `j`, `PgUp`, `Home` | Scroll chat history; selection follows the bottom-most visible message |
| `End` / `G` | Resume following live chat |
| `y` | Copy the selected chat message (or the latest while live) via `wl-copy`, `xclip`, or OSC 52 |
| `i` | Focus chat and start writing a message in the current channel |
| `Enter` | Send the composed message to chat |
| `Esc` | Exit message input while preserving your draft, or return focus to the list |
| `Ctrl+T` | Reconnect to the current channel's IRC chat |

### Layout

| Key | Action |
| --- | --- |
| `Ctrl+W` | Cycle layouts: list + chat → chat only → list only → focus (both hidden) |
| `Ctrl+H` | Show or hide the list panel |
| `Ctrl+L` | Show or hide the chat panel |

The layout is a session preference: the status bar names the active one
(`layout: chat`) and hiding the chat never triggers the narrow-terminal notice. Without an open channel, layout keys keep the list visible and report `No chat yet: open a channel with Enter`. The status bar uses `▶` for playing, `‖` for paused, `✖` for errors and `!` for warnings, so state does not depend on color.

### Video player (`mpv` window)

| Key | Action |
| --- | --- |
| `Space` | Pause or resume video playback |
| `+` / `=` | Increase `mpv` volume by 5% |
| `-` / `_` | Decrease `mpv` volume by 5% |
| `Ctrl+X` | Stop video playback without leaving the app |

Pause and volume work in both windows, the TUI and the `mpv` window, and the status bar follows the volume whichever window changed it. Volume stops at 100% in both. `Ctrl+X` works only in the TUI.

`ttv-tui` replaces the `mpv` key bindings, so every other key in the video window does nothing, including `q`, `f`, and the arrows. The video window has no controls of its own, no on-screen control bar included; everything goes through the TUI. To close the video, press `Ctrl+X` or close its window.

### Application

| Key | Action |
| --- | --- |
| `?` or `F1` | Toggle the keyboard shortcuts overlay (`F1` works even while typing) |
| `Ctrl+Q` | Stop active playback, disconnect chat, and quit |

`Ctrl+S` and `Ctrl+Q` reach the app because `ttv-tui` runs the terminal in raw mode, which disables software flow control: the terminal no longer intercepts `Ctrl+S` to pause output or `Ctrl+Q` to resume it. No extra setup is needed. `Ctrl+F` remains the primary search binding.

## Layout

`ttv-tui` uses a two-panel layout when a channel is open:

- **Left panel.** Navigation list showing Favorites, Recent channels, and Live Categories, Live Channels, or Search Results.
- **Right panel.** Live IRC chat for the active channel, with room mode tags (`slow`, `followers-only`, `subs-only`) in the header and the compose prompt at the bottom.
- **Bottom bar.** Status bar showing the active language (`[en]` or `[pt-BR]`), playback state, volume, channel context, and key hints.

Press `Tab` to move focus between the list and chat panels. The active panel header displays `[focus]`.

Column width adaptation:

- **100+ columns.** Side-by-side split with roughly 40% width for the list and 60% for chat.
- **80 to 99 columns.** Side-by-side split with a compact chat column of at least 34 columns so the list stays readable.
- **Under 80 columns.** In split view, chat is hidden and the notice `Chat hidden (<80 cols) — Ctrl+L opens chat only, or widen the terminal` appears. Press `Ctrl+L` for full-width chat, or widen the terminal to restore the split.


## Experimental embedded video

`ttv-tui --video=sixel` adds a layout that draws the video in the terminal with a chat strip. It is off by default, and every other layout keeps the `mpv` window. The embedded mode is experimental and has not been visually verified in every terminal.

- **Requirements.** A terminal with sixel support and an `mpv` with the `sixel` video output. Both are checked at startup. If either is missing, the app runs as usual and `Ctrl+W` says why.
- **Getting there.** With a channel open, press `Ctrl+W` until the status bar shows `layout: video+chat`, the step after focus. A channel opened there draws in the panel. One already playing in the `mpv` window stays there until you press `r`, which restarts it embedded at the same volume.
- **Video and chat area.** Video uses all available space in `video` mode, minus the chat strip and status bar. `c` hides or shows the chat without restarting the player; in text fields, it still types a letter. In `config.toml`, `video_chat_rows = 8` sets the strip height, from 3 to 30 lines, capped by the available space. `video_chat_position = "bottom"` puts chat below the video; `"top"` puts it above. Missing or invalid values use 8 and `"bottom"`. With chat hidden, video also uses the strip rows.
- **Choosing another channel.** `Esc` or `l` opens the list over the video. Frames stop while it is open and the audio goes on. Choosing a channel or pressing `Esc` returns to the video. `Ctrl+R` goes back to the categories.
- **Getting out.** `Ctrl+W`, `Ctrl+H` or `Ctrl+L` leaves the layout. The audio goes on without a picture and the status bar says so: `r` restarts the channel in the `mpv` window and `Ctrl+X` stops it.
- **Limitations.** At 15 fps, the frame is the largest 16:9 image that fits in the panel, centered. The panel no longer has an 18-line cap; its size follows the terminal and chat strip. In the earlier measurement with 480x270 and 640x360 frames on a real live stream, `mpv` CPU usage rose from about 105% to 125%, and output from 2.0 to 3.2 MB/s. kitty, Alacritty, Terminal.app, Windows and tmux are not supported. A terminal that does not report its cell size gets a video smaller than the panel, so the picture stays clear of the chat.
- **Player startup.** A stalled start with no log line takes 30 s to report an error; a present socket that refuses connections still fails quickly (about 2 s). The streamlink log goes to a file in the session.
- **Waiting for the picture.** After a resize, or when help closes, the picture can take up to about 2.5 s to come back, because `mpv` waits for the next keyframe. A window that shrank leaves the panel blank during that wait, and the list leaves the last frame on screen. The frames also share the line with the keys: on a terminal or SSH link that takes 300 KB/s, a key took about half a second to show.

## Chat

- **Anonymous reading.** Connecting to and reading any channel's chat uses an anonymous Twitch IRC login (`justinfan...`) over TLS (`irc.chat.twitch.tv:6697`) with IRCv3 tags enabled. No account or token is required to read chat.
- **Authenticated sending.** Pressing `i` to compose a message requires a valid token in `config.toml`.
- **Copy message (`y`).** With focus on the chat panel, `y` copies the selected message (the bottom-most visible row, or the latest message while following live) to the system clipboard. The payload includes context, for example `channel #login · YYYY-MM-DD HH:MM · nick: text`, with local time. System lines use their text; messages without a receipt time omit the context. Layout separators and padding are excluded. Mouse selection is not used. Clipboard paths try the tmux buffer first when inside tmux, then (1) `wl-copy` when `WAYLAND_DISPLAY` is set and `wl-copy` is on `PATH` (content persists after the app exits); (2) `xclip -selection clipboard` when `DISPLAY` is set and `xclip` is on `PATH`; (3) OSC 52 written to stderr for SSH/tmux and other hosts without those helpers. If a path fails, the next one is tried. The status bar names the path that worked (`Copied via wl-copy: …` / `xclip` / `OSC 52`), or shows a clear error when every path fails. For the OSC 52 path inside tmux, enable `set-clipboard on` (for example in `~/.tmux.conf`) so the sequence reaches the system clipboard.
- **Chat markers and commands.** Messages from new accounts carry `[new]`. Deleted messages remain visible as `[deleted]` and their text can still be copied. Slash commands are blocked with a warning, except `/me`; the draft is preserved.
- **Rate limits.** Outgoing messages pass through a local rate limiter matching Twitch's limits of 20 messages per 30 seconds globally and 1 message per second per channel. Messages spaced closer than 1 second queue automatically; if the 20-message window is full, excess messages are marked `[discarded: rate limit exceeded (20/30s or 1/s)]`.
- **Delivery tracking and room restrictions.** Each sent message carries a client nonce and displays `[sending]` until confirmed by the server (`[sent]`). If the room rejects a message due to slow mode, followers-only mode, subscribers-only mode, emote-only mode, duplicate message rules (R9K), or a ban/timeout, `ttv-tui` tags the message as `[rejected: <reason>]` inline.

### Moderation

The app does not support bans, timeouts, or message deletion. Twitch discontinued IRC moderation commands; `/ban` and similar commands become public messages, so the app blocks slash commands except `/me`.
Moderation actions require the Helix API with OAuth and a registered Twitch app. Its revocable `client_id` is tied to an account, and revocation would break login for all users at once.
That risk is disproportionate for a product already in a gray area due to ad-free video. Chat supports badges, copying with context, evidence of deleted messages, and raid notices.

## Login and security

Cookie extraction below describes the current Linux implementation. See limitations for integration outside Linux.

`ttv-tui` registers no OAuth application on Twitch and embeds no third-party OAuth client secret. You can authenticate either by importing a token from `twitch-tui` (`--import twt`) or by extracting the session cookie from your web browser (`--from-browser`):

1. **What `--from-browser` reads.** `ttv-tui` locates cookie databases for installed browsers: Brave, Chrome, Chromium, Edge, Vivaldi, Opera, and Firefox, including Flatpak and Snap paths for Firefox. It copies the SQLite database to a temporary directory via `sqlite3 <db> ".backup '<tmp>'"`, falling back to a file copy with `-wal` and `-shm` sidecars, queries only the `auth-token` cookie for `.twitch.tv` / `twitch.tv`, and deletes the temporary copy. The original browser file is never modified.
2. **Reach of the keyring secret and cookie decryption.**
   - **Firefox.** Stores all cookies in plaintext inside `cookies.sqlite`.
   - **Chromium family.** Encrypts cookies with AES-128-CBC (`v10` / `v11`). `ttv-tui` calls `secret-tool` (`libsecret`) to read the browser's OSCrypt Safe Storage password from your desktop session keyring (`application=<browser>` under `chrome_libsecret_os_crypt_password_v2`, or the legacy `<Browser> Safe Storage` service entry), derives the 16-byte AES key with PBKDF2-HMAC-SHA1 (`saltysalt`, 1 iteration), and strips the 32-byte SHA-256 host-key prefix used by newer Chromium database versions (`meta.version >= 24`). Because Chromium uses a single Safe Storage secret per browser installation, that keyring secret can decrypt **all** cookies stored in that browser across every website, not only `twitch.tv`'s `auth-token`. Although `ttv-tui` queries only the `auth-token` row for `twitch.tv` and keeps the derived key in memory only while `login` runs, unlocking the keyring exposes that browser's master cookie key to the process.
3. **Full-account scope and plaintext storage.**
   - **Full session reach.** The `auth-token` cookie from `twitch.tv` is a full first-party web session token for your Twitch account, not an OAuth token restricted to narrow chat scopes (`chat:read` / `chat:edit`). Anyone who obtains this token can act as your account on Twitch until the session is invalidated.
   - **Plaintext on disk (`0600`).** After validating the token against `https://id.twitch.tv/oauth2/validate` to resolve your Twitch username, scopes, and expiration time, `ttv-tui` writes the token in plaintext to `~/.config/ttv-tui/config.toml` (or `$XDG_CONFIG_HOME/ttv-tui/config.toml`) with `0600` file permissions, readable and writable only by your OS user. Any process running under your user account can read this file. The token value is never printed to stdout, stderr, or error logs. On macOS the file is `~/Library/Application Support/ttv-tui/config.toml`; on Windows it is `%APPDATA%\ttv-tui\config.toml`, protected by the inherited profile ACL, without POSIX `0600` guarantees. XDG takes priority on all three systems.
4. **When to prefer `--import twt`.** If you already have `twitch-tui` configured at `~/.config/twt/config.toml` or prefer using a chat-scoped OAuth token without granting access to your browser cookie database and Safe Storage keyring secret, run `ttv-tui login --import twt` instead of `--from-browser`.
5. **How to revoke.** Deleting `config.toml` only removes the local file; the token remains valid on Twitch until revoked on the server. To invalidate the session on Twitch's servers, open `twitch.tv` and use **Settings -> Security and Privacy -> Sign Out Everywhere**, or log out of the browser session from which the token was extracted, or revoke the connection under **Settings -> Connections** if imported via `--import twt`. Then delete your local configuration file:

On Linux (on macOS/Windows remove the file indicated by `doctor`):

```bash
rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/ttv-tui/config.toml"
```

See [SECURITY.md](SECURITY.md) for vulnerability reporting and revocation instructions.

## Troubleshooting

Run `ttv-tui doctor --lang en` (or `--lang pt-BR`) before reporting a failure. It shows app version, OS/architecture, presence and numeric version of `streamlink`/`mpv`, effective directories and token presence as YES/NO only. It does not validate the token over the network. Unreadable configuration is reported without echoing its error or contents. Missing dependencies or unreadable state/configuration return exit code 1; a completed diagnostic returns 0.

“Clean” means no pending records; “pending” can also mean an active instance. The command does not modify records or test past IPC, terminal, audio or video. A tool found whose version probe fails is reported as version unavailable.


- **`Channel offline: pick another channel`.** `streamlink` reported no playable streams for that login. Select another live channel or press `r` to refresh the channel list.
- **`streamlink not found` / `mpv not found`.** Install `streamlink` and `mpv` via your distribution's package manager and confirm both are in `PATH`.
- **`sqlite3 not found in PATH` during login.** Install the `sqlite` / `sqlite3` CLI package, such as `sudo pacman -S sqlite` or `sudo apt install sqlite3`.
- **`keyring locked or unavailable` during login.** Unlock your desktop session keyring or install `libsecret` (`secret-tool`), sign in with Firefox and pass `--browser firefox`, or import a token with `ttv-tui login --import twt`.
- **`Token expired or invalid`.** Browser session tokens expire when your web session ends. Log in to `twitch.tv` in your browser if needed and re-run `ttv-tui login --from-browser` or `ttv-tui login --import twt`.
- **Orphaned player prompt on startup (`A player from a previous session is still running...`).** If a previous `ttv-tui` process was killed abruptly with `SIGKILL`, its `streamlink` and `mpv` process group may still be playing. On the next launch, `ttv-tui` verifies the recorded PID and socket in `~/.local/state/ttv-tui/players/` and prompts `[Y/n]` to stop the leftover player before opening the UI.
- **Chat is hidden.** In split view below 80 columns, press `Ctrl+L` to open full-width chat, or widen the terminal or tmux pane to restore the split.
- **No video window or no audio from `mpv`.** `ttv-tui` launches `mpv` with `--no-config --no-input-default-bindings --no-osc --volume-max=100 --force-window=immediate --hwdec=auto-safe`. Verify that your Wayland or X11 display session (`DISPLAY` or `WAYLAND_DISPLAY`) and audio server (PipeWire / PulseAudio) are running, and check that `Volume` in the status bar is not `vol 0` (press `+` to raise volume). Press `?` after a player failure to view the `Last player error` line reported by `streamlink` or `mpv`.
- **Colors and `NO_COLOR`.** Set `NO_COLOR` to any non-empty value in the terminal environment (for example `NO_COLOR=1 ttv-tui`) to turn color off without changing anything else; `TERM=dumb` and `CLICOLOR=0` have the same effect. No color SGR sequence is emitted (bold, reverse, and underline remain), so symbols keep carrying the state: the selection stays on `▸`, favorites keep `★`, live channels keep `●` and `[live]`, and offline channels keep `[offline]`. `NO_COLOR` is an environment variable, not a `config.toml` key.

## Development

Run the build, `go vet`, test suite, race detector, and `golangci-lint` `v2.15.0` matching [`.github/workflows/ci.yml`](.github/workflows/ci.yml):

```bash
TMPDIR=/tmp go build ./...
TMPDIR=/tmp go vet ./...
TMPDIR=/tmp go test -count=1 ./...
TMPDIR=/tmp go test -race -count=1 ./internal/player ./internal/chat ./internal/ui ./internal/store
TMPDIR=/tmp go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run
```

- **Why `TMPDIR=/tmp` is used.** Tests in `internal/player` create temporary directories via `t.TempDir()` and bind per-session `mpv` Unix domain sockets (`i`) inside them. The Linux kernel limits `sockaddr_un.sun_path` to 108 bytes, and `internal/player` enforces a 100-byte ceiling. When the parent environment sets `TMPDIR` to a long directory path, socket paths inside `t.TempDir()` can exceed that byte limit and fail to bind. Setting `TMPDIR=/tmp` keeps test socket paths short.
- **Pinned `golangci-lint` version (`v2.15.0`).** CI runs `golangci-lint` `v2.15.0`. If you already have `golangci-lint` `v2.15.0` installed in `PATH`, you can run `TMPDIR=/tmp golangci-lint run` directly.

### Paths by OS

`XDG_CONFIG_HOME`, `XDG_STATE_HOME` and `XDG_RUNTIME_DIR` take priority on every OS, each with a `ttv-tui` suffix. Without those variables:

| OS | Configuration (token) | State | Runtime |
| --- | --- | --- | --- |
| Linux | `~/.config/ttv-tui/config.toml` | `~/.local/state/ttv-tui` | `<OS temp>/ttv-tui` |
| macOS | `~/Library/Application Support/ttv-tui/config.toml` | `~/Library/Application Support/ttv-tui/state` | `$TMPDIR/ttv-tui` (otherwise OS temp) |
| Windows | `%APPDATA%\ttv-tui\config.toml` | `%LOCALAPPDATA%\ttv-tui` | `%LOCALAPPDATA%\ttv-tui\run` |

The state directory contains `state.json` (favorites and up to 50 recents) and `players/player-<pid>-<gen>.json`. Each session creates `<runtime>/<gen>-*/mpv-input.conf`; Unix also creates socket `i` in a `0700` directory, with a `0600` key file. Windows IPC uses a named pipe without a socket file. If the directory cannot be used, the fallback is `<OS temp>/ttv-tui-<pid>-<gen>/`. The token uses `0600` on Unix and the inherited profile ACL on Windows; see [SECURITY.md](SECURITY.md).

If `state.json` is corrupted, `ttv-tui` backs it up to `state.json.bak` and starts with empty favorites and recents.

Architecture Decision Records are documented in [`docs/adr/`](docs/adr/).

## Limitations and roadmap

Current limitations and v1.8 integration:

- **Login outside Linux pending in this checkout.** Profile discovery still uses Linux paths and `login --token` is not available yet. The v1.8 authentication scope calls for Firefox and hidden token input; Windows Chromium (App-Bound Encryption) is outside scope. Implementation and validation belong to the authentication ticket.
- **No chat moderation commands.** Deleting messages, timeouts, and bans are not implemented.
- **Text-only emotes.** Chat renders text and Unicode; graphical Twitch, BTTV, and 7TV emotes are not rendered as images in the terminal.
- **Live streams only.** VODs and clips are not supported.
- **Local favorites and recents.** Favorite channels are stored locally in `state.json` rather than synced from your Twitch followed channels list, which requires a registered Helix OAuth client ID.

## License

MIT. See [LICENSE](LICENSE).
