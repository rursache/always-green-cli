# always-green-cli

Keep your Slack status green from a laptop or VPS you leave on.

Auth tokens stay local and private. Nothing is sent to a third-party service.

## Install

```bash
brew tap rursache/tap && brew trust rursache/tap && brew install always-green
```

## Usage

```
always-green
```

On first run, pick how to add your session:

1. Slack desktop app (reads Keychain; skip this on MDM-locked Macs)
2. Paste from Chrome (default)

The daemon starts **always on**. Close the terminal; it keeps running.

```
always-green status
always-green stop
always-green tui          # schedules, pause, add another workspace
always-green reauth       # refresh tokens Slack has expired
always-green snippet      # Chrome console helper for xoxc
```

## When tokens expire

Slack rotates session tokens every few days.

Workspaces imported from the **Slack desktop app** refresh themselves: the daemon
re-reads the app and carries on, no prompt.

Workspaces added by **pasting from Chrome** cannot, so always-green tells you:
a desktop notification when it happens, `always-green status` exits non-zero,
and the next `always-green` run drops you into the paste flow. You can also run
`always-green reauth` directly.

## Chrome tokens

No extension.

1. Open [app.slack.com](https://app.slack.com) and sign in
2. DevTools → Console, paste `always-green snippet`, Enter (copies `xoxc`)
3. DevTools → Application → Cookies → `https://app.slack.com` → cookie `d` (starts with `xoxd-`)

The `d` cookie is HttpOnly, so JavaScript cannot read it.

## License

[MIT](LICENSE)
