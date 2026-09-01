//go:build linux

package desktop

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// keyringTimeout bounds the whole password hunt, not each lookup: several
// tools are tried in turn and a locked keyring can stall every one of them,
// and the daemon's own refresh budget is only 60s
const keyringTimeout = 15 * time.Second

const osCryptSchema = "chrome_libsecret_os_crypt_password_v2"

// safeStoragePasswords collects every candidate the Slack app may have
// encrypted its cookies with. Electron hands cookie encryption to Chromium's
// os_crypt, which keeps a random password in the Secret Service (GNOME
// Keyring, KeePassXC) or in KWallet and only falls back to the fixed
// "peanuts" when no keyring is available, so on most desktops the fixed
// password alone decrypts nothing
func safeStoragePasswords() ([][]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keyringTimeout)
	defer cancel()
	var out [][]byte
	seen := map[string]struct{}{}
	add := func(pw string) {
		pw = strings.TrimSpace(pw)
		if pw == "" {
			return
		}
		if _, dup := seen[pw]; dup {
			return
		}
		seen[pw] = struct{}{}
		out = append(out, []byte(pw))
	}
	// os_crypt stores the item with application set to the product name,
	// which Electron passes through as the app name verbatim
	for _, app := range []string{"Slack", "slack"} {
		if raw, err := keyringRun(ctx, "secret-tool", "lookup", "application", app); err == nil {
			add(raw)
		}
	}
	// a Flatpak or renamed install can carry another product name, so also
	// walk every os_crypt item and keep the ones whose application names
	// Slack; the label is the generic "Chromium Safe Storage" for every
	// Electron app, so it cannot tell them apart
	if raw, err := keyringRun(ctx, "secret-tool", "search", "--all", "--unlock", "xdg:schema", osCryptSchema); err == nil {
		for _, pw := range secretsForApplication(raw, "slack") {
			add(pw)
		}
	}
	// an unbranded Chromium build such as Electron keeps its KWallet entry
	// under the generic Chromium names
	if raw, err := keyringRun(ctx, "kwallet-query", "-f", "Chromium Keys", "-r", "Chromium Safe Storage", "kdewallet"); err == nil {
		add(raw)
	}
	add("peanuts")
	return out, nil
}

func keyringRun(ctx context.Context, name string, args ...string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	raw, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func cookieRounds() int { return 1 }
