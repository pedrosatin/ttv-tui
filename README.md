# ttv-tui

[![CI](https://github.com/pedrosatin/ttv-tui/actions/workflows/ci.yml/badge.svg)](https://github.com/pedrosatin/ttv-tui/actions/workflows/ci.yml)

[Português (Brasil)](docs/README.pt-BR.md)

Terminal UI for Twitch on Linux: browse live categories and channels, watch streams through `streamlink` and `mpv` with ad filtering and in-app playback controls, and participate in IRC chat without opening a browser.

![Watching a live channel with side-by-side chat](docs/img/playing-chat.svg)

## Install

### Release binary (v0.1.0, Linux amd64 / arm64)

Download the archive for your architecture (`linux_amd64` or `linux_arm64`) and `checksums.txt` from the [v0.1.0 release](https://github.com/pedrosatin/ttv-tui/releases/tag/v0.1.0), verify the SHA-256 digest, and place the binary in `~/.local/bin`:

```bash
ARCH=amd64 # or arm64
curl -LO "https://github.com/pedrosatin/ttv-tui/releases/download/v0.1.0/ttv-tui_0.1.0_linux_${ARCH}.tar.gz"
curl -LO "https://github.com/pedrosatin/ttv-tui/releases/download/v0.1.0/checksums.txt"
sha256sum --ignore-missing -c checksums.txt

mkdir -p ~/.local/bin
tar -xzf "ttv-tui_0.1.0_linux_${ARCH}.tar.gz" -C ~/.local/bin ttv-tui
```

Ensure `~/.local/bin` is in your `PATH`.

### Go install

Requires Go 1.27 or newer:

```bash
go install github.com/pedrosatin/ttv-tui/cmd/ttv-tui@v0.1.0
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
CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=0.1.0" -o ~/.local/bin/ttv-tui ./cmd/ttv-tui
```

## Requirements

### Required (video playback)

- Linux.
- `streamlink` in `PATH` to negotiate stream playback and filter ad segments (without it, there is no video).
- `mpv` in `PATH` to decode and display video in its own window while exposing a local JSON IPC socket (without it, there is no video).

### Optional (login and chat)

- `sqlite3` in `PATH`. `ttv-tui` runs the `sqlite3` command-line binary only during `ttv-tui login --from-browser` to copy and query your browser's cookie database (using SQLite's online `.backup` command so an open browser lock is not contested). Not needed if you browse and watch anonymously or import tokens via `ttv-tui login --import twt`.
- A supported browser logged into `twitch.tv` (Brave, Chrome, Chromium, Edge, Vivaldi, Opera, or Firefox) if you want to send chat messages. For Chromium-family browsers, `secret-tool` (`libsecret`) must also be in `PATH` to read the OSCrypt key from the desktop keyring. Browsing categories, watching streams, and reading chat work anonymously without logging in.

## Quick start

1. Extract your Twitch session token from an installed browser (optional, needed only to send chat messages):

```bash
ttv-tui login --from-browser
```

If you have multiple browser profiles, pass `--browser` and `--profile` directly or pick from the interactive prompt:

```bash
ttv-tui login --from-browser --browser brave --profile Default
```

If you already use `twitch-tui` (`twt`), you can import its token from `~/.config/twt/config.toml` instead:

```bash
ttv-tui login --import twt
```

2. Launch the interface:

```bash
ttv-tui
```

The UI language follows `--lang` (`en` or `pt-BR`), the `language` setting in `config.toml`, or your `LC_ALL` / `LC_MESSAGES` / `LANG` environment variables, defaulting to English.

## Walkthrough

When `ttv-tui` starts, it loads the top live categories on Twitch:

![Live categories list](docs/img/categories.svg)

Select a category with `Enter` to list its live channels, then press `Enter` on a channel to launch video playback in `mpv` and join the channel's IRC chat in the right panel:

![Playing a channel with chat open](docs/img/playing-chat.svg)

Press `?` or `F1` at any time to display the in-app keyboard shortcuts overlay:

![Keyboard shortcuts overlay](docs/img/help.svg)

For a step-by-step tour covering installation, authentication, channel switching, favorites, chat, narrow terminals, and shutdown, read the [full walkthrough](docs/walkthrough.md).

## Keybindings

All shortcuts below match the in-app help overlay (`?` or `F1`):

### List navigation and search

| Key | Action |
| --- | --- |
| `↑` / `↓` or `j` / `k` | Move selection up or down in the list (loads more items at the bottom) |
| `Enter` | Open the highlighted category or start watching the highlighted channel |
| `f` | Toggle the highlighted channel in Favorites |
| `/` | Filter the current list locally (`Enter` keeps filter, `Esc` clears) |
| `Ctrl+S` | Search channels across Twitch (reuses active `/` filter text if present) |
| `r` | Retry loading the current list or retry a failed video playback |
| `Esc` | Clear active filter, or step back from channels / search results to categories |
| `Ctrl+R` | Return to Live Categories from anywhere, even while typing |
| `Tab` | Switch keyboard focus between the list panel and the chat panel |

### Chat

| Key | Action |
| --- | --- |
| `i` | Focus chat and start writing a message in the current channel |
| `Enter` | Send the composed message to chat |
| `Esc` | Exit message input (preserving your draft) or return focus to the list |
| `Ctrl+T` | Reconnect to the current channel's IRC chat |

### Video player (`mpv` window)

| Key | Action |
| --- | --- |
| `Space` | Pause or resume video playback |
| `+` / `=` | Increase `mpv` volume by 5% |
| `-` / `_` | Decrease `mpv` volume by 5% |
| `Ctrl+X` | Stop video playback without leaving the app |

### Application

| Key | Action |
| --- | --- |
| `?` or `F1` | Toggle the keyboard shortcuts overlay (`F1` works even while typing) |
| `Ctrl+C` or `Ctrl+Q` | Stop active playback, disconnect chat, and quit |

## Layout

`ttv-tui` uses a two-panel layout when a channel is open:

- **Left panel**: navigation list (Favorites, Recent channels, and Live Categories, Live Channels, or Search Results).
- **Right panel**: live IRC chat for the active channel, with room mode tags (`slow`, `followers-only`, `subs-only`) in the header and the compose prompt at the bottom.
- **Bottom bar**: status bar showing the active language (`[en]` or `[pt-BR]`), playback state, volume, channel context, and key hints.

Press `Tab` to move focus between the list and chat panels. The active panel header displays `[focus]`.

**Column width adaptation:**
- **100+ columns**: side-by-side split with ~40% width for the list and ~60% for chat.
- **80 to 99 columns**: side-by-side split with a compact chat column (at least 34 columns) so the list stays readable.
- **Under 80 columns**: the chat panel hides automatically and a notice (`! Chat hidden (<80 cols)`) appears above the status bar. Widen the terminal to 80 columns or more to restore chat.

![Narrow terminal at 79 columns with hidden chat notice](docs/img/narrow-79cols.svg)

## Chat

- **Anonymous reading**: connecting to and reading any channel's chat uses an anonymous Twitch IRC login (`justinfan...`) over TLS (`irc.chat.twitch.tv:6697`) with IRCv3 tags enabled. No account or token is required to read chat.
- **Authenticated sending**: pressing `i` to compose a message requires a valid token in `config.toml`.
- **Rate limits**: outgoing messages pass through a local rate limiter matching Twitch's limits of **20 messages per 30 seconds** globally and **1 message per second** per channel. Messages spaced closer than 1 second queue automatically; if the 20-message window is full, excess messages are marked `[discarded: rate limit exceeded (20/30s or 1/s)]`.
- **Delivery tracking and room restrictions**: each sent message carries a client nonce and displays `[sending]` until confirmed by the server (`[sent]`). If the room rejects a message due to slow mode, followers-only mode, subscribers-only mode, emote-only mode, duplicate message rules (R9K), or a ban/timeout, `ttv-tui` tags the message as `[rejected: <reason>]` inline.

## Login and security

`ttv-tui` does not register an OAuth application on Twitch and embeds no third-party OAuth client secret. Instead, `ttv-tui login --from-browser` reuses the session from your own web browser:

1. **What is read**: `ttv-tui` locates cookie databases for installed browsers (Brave, Chrome, Chromium, Edge, Vivaldi, Opera, and Firefox, including Flatpak and Snap paths for Firefox). It copies the SQLite database to a temporary directory via `sqlite3 <db> ".backup '<tmp>'"` and queries only the `auth-token` cookie for `.twitch.tv` / `twitch.tv`. The original browser file is never modified.
2. **How it is decrypted**:
   - **Firefox**: stores the cookie value in plaintext inside `cookies.sqlite`.
   - **Chromium family**: encrypts cookies with AES-128-CBC (`v10` / `v11`). `ttv-tui` calls `secret-tool` (`libsecret`) to read the browser's Safe Storage password from your desktop session keyring, derives the AES key with PBKDF2-HMAC-SHA1 (`saltysalt`, 1 iteration, 16 bytes), and strips the 32-byte SHA-256 host-key prefix used by newer Chromium database versions (`meta.version >= 24`).
3. **Validation and storage**: the extracted token is validated against `https://id.twitch.tv/oauth2/validate` to resolve your Twitch username, scopes, and expiration time. It is then written in plaintext to `~/.config/ttv-tui/config.toml` (or `$XDG_CONFIG_HOME/ttv-tui/config.toml`) with `0600` file permissions (readable and writable only by your user). The token value is never printed to stdout, stderr, or error logs.
4. **How to revoke**: to invalidate the token on Twitch's servers, use **Settings -> Security and Privacy -> Sign Out Everywhere** (or log out of that browser session) on `twitch.tv`, then delete your local configuration file:

```bash
rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/ttv-tui/config.toml"
```

## Troubleshooting

- **`Channel offline: pick another channel`**: `streamlink` reported no playable streams for that login. Select another live channel or press `r` to refresh the channel list.
- **`streamlink not found` / `mpv not found`**: install `streamlink` and `mpv` via your distribution's package manager and confirm both are in `PATH`.
- **`sqlite3 not found in PATH` during login**: install the `sqlite` / `sqlite3` CLI package (e.g. `sudo pacman -S sqlite` or `sudo apt install sqlite3`).
- **`keyring locked or unavailable` during login**: unlock your desktop session keyring or install `libsecret` (`secret-tool`), or sign in with Firefox and pass `--browser firefox`.
- **`Token expired or invalid`**: browser session tokens eventually expire when your web session ends. Log in to `twitch.tv` in your browser if needed and re-run `ttv-tui login --from-browser`.
- **Orphaned player prompt on startup (`A player from a previous session is still running...`)**: if a previous `ttv-tui` process was killed abruptly (for example with `SIGKILL`), its `streamlink`/`mpv` process group may still be playing. On the next launch, `ttv-tui` verifies the recorded PID and socket in `~/.local/state/ttv-tui/players/` and prompts `[Y/n]` to stop the leftover player before opening the UI.
- **Chat is hidden**: `ttv-tui` hides the right-hand chat panel when the terminal width is below 80 columns. Resize your terminal window or tmux pane to at least 80 columns.
- **No video window or no audio from `mpv`**: `ttv-tui` launches `mpv` with `--no-config --force-window=immediate --hwdec=auto-safe`. Verify that your Wayland or X11 display session (`DISPLAY` or `WAYLAND_DISPLAY`) and audio server (PipeWire / PulseAudio) are running, and check that `Volume` in the status bar is not `vol 0` (press `+` to raise volume). Press `?` after a player failure to view the `Last player error` line reported by `streamlink` or `mpv`.

## Development

Run the test suite, race detector, `go vet`, and `golangci-lint` (matching `.github/workflows/ci.yml`):

```bash
TMPDIR=/tmp go build ./...
TMPDIR=/tmp go vet ./...
TMPDIR=/tmp go test -count=1 ./...
TMPDIR=/tmp go test -race -count=1 ./internal/player ./internal/chat ./internal/ui ./internal/store
golangci-lint run
```

### XDG files and directories

`ttv-tui` stores configuration, state, and runtime sockets in standard XDG locations:

| Purpose | Path | Fallback |
| --- | --- | --- |
| Configuration (`config.toml`, mode `0600`) | `$XDG_CONFIG_HOME/ttv-tui/config.toml` | `~/.config/ttv-tui/config.toml` |
| Persistent state (`state.json`: favorites and up to 50 recent channels) | `$XDG_STATE_HOME/ttv-tui/state.json` | `~/.local/state/ttv-tui/state.json` |
| Live player registry (orphan detection records) | `$XDG_STATE_HOME/ttv-tui/players/player-<pid>-<gen>.json` | `~/.local/state/ttv-tui/players/player-<pid>-<gen>.json` |
| Per-session `mpv` IPC socket (`i`, mode `0700` dir) | `$XDG_RUNTIME_DIR/ttv-tui/<gen>-*/i` | `/tmp/ttv-tui-<pid>-<gen>/i` |

If `state.json` is corrupted, `ttv-tui` backs it up to `state.json.bak` and starts with empty favorites and recents.

Architecture Decision Records are documented in [`docs/adr/`](docs/adr/).

## Limitations and roadmap

Current limitations in `v0.1.0`:

- **Linux only**: process-group supervision (`/proc` checks, `Setpgid`), keyring lookup (`secret-tool`), and browser cookie paths target Linux.
- **No chat moderation commands**: deleting messages, timeouts, and bans are not implemented.
- **Text-only emotes**: chat renders text and Unicode; graphical Twitch/BTTV/7TV emotes are not rendered as images in the terminal.
- **Live streams only**: VODs and clips are not supported.
- **Local favorites and recents**: favorite channels are stored locally in `state.json` rather than synced from your Twitch followed channels list (which requires a registered Helix OAuth client ID).

## License

MIT. See [LICENSE](LICENSE).
