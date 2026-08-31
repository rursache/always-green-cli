# always-green-cli

Keep your Slack status green from a laptop or VPS you leave on.

Auth tokens stay local and private. Nothing is sent to a third-party service.

## Install

```bash
brew tap rursache/tap && brew trust rursache/tap && brew install always-green-cli
```

The binary is `always-green-cli`; `always-green` works too as an alias.

## Usage

```
always-green-cli
```

On first run, pick how to add your session:

1. Slack desktop app (reads Keychain; skip this on MDM-locked Macs)
2. Paste from Chrome (default)

The daemon starts **always on**. Close the terminal; it keeps running.

```
always-green-cli status
always-green-cli stop
always-green-cli tui            # schedules, pause, add another workspace
always-green-cli reauth         # refresh tokens Slack has expired
always-green-cli autostart off  # stop launching at login
always-green-cli snippet        # Chrome console helper for xoxc
```

## Launch at login

Set up automatically on first run, so a reboot does not quietly leave you
offline. It is a per-user login item, no sudo and nothing outside your home
directory: a LaunchAgent on macOS, a systemd user unit on Linux.

```
always-green-cli autostart status
always-green-cli autostart off
always-green-cli autostart on
```

Starting at login is not the same as staying started: `always-green-cli stop`
stays stopped until you start it again. `always-green-cli uninstall` removes
the login item along with the daemon.

## When tokens expire

Slack hands out two credentials, and they do not expire together:

- the `d` cookie (`xoxd-`) is the real session, and lasts months
- the `xoxc` token is minted from it and rotates every few days

always-green refreshes the short-lived half on its own, so a rotation costs you
nothing. Workspaces from the **Slack desktop app** re-read the app. Workspaces
**pasted from Chrome** mint a new `xoxc` from the saved `d` cookie.

You only get involved when the `d` cookie itself expires, which normally means
you signed out, changed your password, or an admin ended the session. Then
you get a desktop notification, `always-green status` exits non-zero, and the
next `always-green` run walks you through pasting fresh tokens. `always-green
reauth` does the same on demand.

## Chrome tokens

No extension.

1. Open [app.slack.com](https://app.slack.com) and sign in
2. DevTools → Console, paste `always-green-cli snippet`, Enter (copies `xoxc`)
3. DevTools → Application → Cookies → `https://app.slack.com` → cookie `d` (starts with `xoxd-`)

The `d` cookie is HttpOnly, so JavaScript cannot read it.

Both are saved encrypted under `~/.always-green`, along with your workspace
domain, which is what lets always-green mint a replacement `xoxc` later without
asking you again.

## A caveat worth knowing

This drives Slack with a browser session outside a browser, which is not what
Slack's session model intends. Slack does watch for automated cookie reuse and
can invalidate a session it finds suspicious. Presence checks are deliberately
infrequent for that reason, but the risk is not zero: if your workplace treats
this as circumvention, that is a conversation to have with them, not with a
tool.

## License

[MIT](LICENSE)
