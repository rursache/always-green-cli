# always-green

Keep your Slack status green from a laptop or VPS you leave on.

Tokens stay on that machine. Nothing is sent to a third-party service.

## Install

Needs Go 1.22+ on macOS or Linux.

```
git clone <repo>
cd always-green
make build
```

Cross-compile with `./build.sh` (writes `dist/`).

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
always-green snippet      # Chrome console helper for xoxc
```

## Chrome tokens

No extension.

1. Open [app.slack.com](https://app.slack.com) and sign in
2. DevTools → Console, paste `always-green snippet`, Enter (copies `xoxc`)
3. DevTools → Application → Cookies → `https://app.slack.com` → cookie `d` (starts with `xoxd-`)

The `d` cookie is HttpOnly, so JavaScript cannot read it.

## License

[MIT](LICENSE)
