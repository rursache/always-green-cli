package desktop

import "strings"

// secretsLabelled picks the secrets out of "secret-tool search --all" output
// for the items whose label matches. Items are printed as a bracketed object
// path followed by "key = value" lines, with the secret on its own line
func secretsLabelled(raw, label string) []string {
	var out []string
	var itemLabel, itemSecret string
	flush := func() {
		if itemLabel == label && itemSecret != "" {
			out = append(out, itemSecret)
		}
		itemLabel, itemSecret = "", ""
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
		case "label":
			itemLabel = val
		case "secret":
			itemSecret = val
		}
	}
	flush()
	return out
}
