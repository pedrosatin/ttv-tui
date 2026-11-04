# Security policy

## Reporting a vulnerability

Report security vulnerabilities privately through [GitHub Private Vulnerability Reporting](https://github.com/pedrosatin/ttv-tui/security/advisories/new) on this repository (under the **Security** tab, select **Report a vulnerability**).

Do not open public issues, pull requests, or discussions for undisclosed vulnerabilities. Never include live Twitch tokens, cookie databases, or keyring secrets in a report.

Include the affected version or commit hash, Linux distribution, steps to reproduce, and the impact of the issue.

## Credential and keyring reach

`ttv-tui` can run anonymously for browsing categories, watching streams, and reading IRC chat. Authentication is optional and used only to send chat messages.

When you authenticate, understand the reach of each login path:

1. **Browser cookie extraction (`ttv-tui login --from-browser`)**
   - **Keyring reach on Chromium-family browsers.** Brave, Chrome, Chromium, Edge, Vivaldi, and Opera encrypt cookies on Linux using a single per-browser OSCrypt Safe Storage secret stored in the desktop session keyring. `ttv-tui` calls `secret-tool` (`libsecret`) to read that password and derives the 16-byte AES-128-CBC key (`PBKDF2-HMAC-SHA1`, salt `saltysalt`, 1 iteration). That single key can decrypt **every** cookie stored by that browser across all websites, not only `twitch.tv`'s `auth-token`. Although `ttv-tui` queries only the `auth-token` row for `.twitch.tv` / `twitch.tv` from a temporary SQLite copy and discards the key when `login` finishes, unlocking the keyring exposes the browser's master cookie secret to the process.
   - **Firefox cookie database.** Firefox stores all cookies in plaintext inside `cookies.sqlite`. `ttv-tui` copies the database to a temporary directory, reads only the `auth-token` row for `.twitch.tv` / `twitch.tv`, and deletes the temporary copy.
   - **Full-account session token.** The `auth-token` cookie from `twitch.tv` is a first-party web session token for your entire Twitch account, not an OAuth token restricted to narrow chat scopes (`chat:read` / `chat:edit`). Anyone who obtains this token can act as your account on Twitch until the session is invalidated.

2. **Importing from `twitch-tui` (`ttv-tui login --import twt`)**
   - When you already have a chat-scoped OAuth token in `~/.config/twt/config.toml` or want to avoid reading browser cookie databases and the Safe Storage keyring secret, prefer `ttv-tui login --import twt`.

3. **Plaintext storage on disk**
   - `ttv-tui` saves the validated token in plaintext at `~/.config/ttv-tui/config.toml` (or `$XDG_CONFIG_HOME/ttv-tui/config.toml`) with `0600` file permissions. Any process running under your OS user account can read this file.
   - `ttv-tui` never prints the token to stdout, stderr, error messages, or logs.

## How to revoke a token

Deleting the local configuration file removes the token from disk, while the session remains valid on Twitch until invalidated on the server:

1. **Invalidate the token on Twitch:**
   - For a browser-extracted `auth-token` (`--from-browser`), open `https://www.twitch.tv`, go to **Settings -> Security and Privacy -> Sign Out Everywhere**, or log out of the browser session from which the cookie was copied.
   - For a token imported via `--import twt`, revoke the application under **Settings -> Connections** on `https://www.twitch.tv` (or use **Sign Out Everywhere**).
2. **Delete the local configuration file:**

```bash
rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/ttv-tui/config.toml"
```

If you imported the token from `twitch-tui`, also remove or rotate `~/.config/twt/config.toml`.
