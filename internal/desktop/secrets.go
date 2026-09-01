package desktop

import "strings"

// secretsForApplication picks the secrets out of "secret-tool search --all"
// output for the os_crypt items whose application attribute names app,
// matched case-insensitively as a substring so a renamed or Flatpak build
// still qualifies. Items are printed as a bracketed object path followed by
// "key = value" lines, with the secret on its own line
func secretsForApplication(raw, app string) []string {
	app = strings.ToLower(app)
	var out []string
	var itemApp, itemSecret string
	flush := func() {
		if itemSecret != "" && strings.Contains(strings.ToLower(itemApp), app) {
			out = append(out, itemSecret)
		}
		itemApp, itemSecret = "", ""
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			flush()
			continue
		}
		key, val, ok := strings.Cut(line, " = ")
		if !ok {
			continue
		}
		switch key {
		case "attribute.application":
			itemApp = val
		case "secret":
			itemSecret = val
		}
	}
	flush()
	return out
}
