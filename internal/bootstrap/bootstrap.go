package bootstrap

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rursache/always-green/internal/desktop"
	"github.com/rursache/always-green/internal/importws"
	"github.com/rursache/always-green/internal/slackx"
	"github.com/rursache/always-green/internal/store"

	"golang.org/x/term"
)

func Ensure(st *store.Store, out io.Writer) error {
	list, err := st.Workspaces()
	if err != nil {
		return err
	}
	if len(list) > 0 {
		return nil
	}
	return firstRun(st, out)
}

// EnsureValid walks the user through re-auth for any workspace whose tokens
// Slack has rejected. Desktop-imported workspaces heal themselves in the
// daemon, so anything still flagged here needs a human paste.
func EnsureValid(st *store.Store, out io.Writer) error {
	list, err := st.Workspaces()
	if err != nil {
		return err
	}
	var dead []store.Workspace
	for _, ws := range list {
		if ws.TokenInvalid {
			dead = append(dead, ws)
		}
	}
	if len(dead) == 0 {
		return nil
	}
	fmt.Fprintln(out)
	for _, ws := range dead {
		fmt.Fprintf(out, "Slack expired the tokens for %s\n", ws.Name)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("run always-green reauth in a terminal to paste fresh tokens")
	}
	return Reauth(st, out, false)
}

// Reauth re-reads tokens: straight from the Slack app when that is where they
// came from, otherwise a manual Chrome paste. With force it refreshes every
// workspace, not just the ones Slack has already rejected.
func Reauth(st *store.Store, out io.Writer, force bool) error {
	list, err := st.Workspaces()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return Ensure(st, out)
	}
	remaining := 0
	for _, ws := range list {
		if !ws.TokenInvalid && !force {
			continue
		}
		if ws.Source != store.SourceDesktop {
			remaining++
			continue
		}
		fmt.Fprintf(out, "Re-reading %s from the Slack app...\n", ws.Name)
		if err := importws.RefreshDesktop(st, ws.TeamID); err != nil {
			fmt.Fprintf(out, "  could not read it (%v), paste tokens instead\n", err)
			remaining++
			continue
		}
		fmt.Fprintf(out, "  refreshed %s\n", ws.Name)
	}
	if remaining == 0 {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("run always-green reauth in a terminal to paste fresh tokens")
	}
	return fromChrome(st, out, bufio.NewReader(os.Stdin))
}

func firstRun(st *store.Store, out io.Writer) error {
	fmt.Fprintln(out, "First run: we need your Slack session tokens")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  1) Slack desktop app")
	fmt.Fprintln(out, "     Reads Keychain (Slack Safe Storage)")
	fmt.Fprintln(out, "     Often blocked on MDM-locked Macs")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  2) Paste from Chrome")
	fmt.Fprintln(out, "     Console snippet for xoxc, then copy the d cookie")
	fmt.Fprintln(out, "     No extension, no Keychain")
	fmt.Fprintln(out)

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("no saved tokens and stdin is not a terminal. Run always-green in a terminal and pick 1 or 2")
	}

	in := bufio.NewReader(os.Stdin)
	choice, err := readLine(in, out, "Choice [2]: ")
	if err != nil {
		return err
	}
	switch strings.TrimSpace(choice) {
	case "1":
		return fromDesktop(st, out, in)
	default:
		return fromChrome(st, out, in)
	}
}

// in is threaded through rather than rebuilt: a second bufio.Reader over the
// same stdin discards whatever the first one had already buffered, which
// swallows the rest of a paste the user has already typed
func fromDesktop(st *store.Store, out io.Writer, in *bufio.Reader) error {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Reading the Slack desktop app...")
	fmt.Fprintln(out, "macOS may ask for Keychain access. If MDM blocks it, pick option 2 instead")
	n, err := importAll(st, out)
	if n > 0 {
		return nil
	}
	if err != nil {
		fmt.Fprintf(out, "Desktop import failed: %v\n\n", err)
	} else {
		fmt.Fprintln(out, "No workspace tokens in the Slack app")
		fmt.Fprintln(out)
	}
	return fromChrome(st, out, in)
}

func fromChrome(st *store.Store, out io.Writer, in *bufio.Reader) error {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Chrome (use the Slack web app, not the desktop app):")
	fmt.Fprintln(out, "  1. Open https://app.slack.com and sign in")
	fmt.Fprintln(out, "  2. DevTools → Console, paste this, press Enter")
	fmt.Fprintln(out)
	fmt.Fprintln(out, ChromeSnippet)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  3. That copies xoxc. The d cookie is HttpOnly, so JS cannot read it")
	fmt.Fprintln(out, "  4. DevTools → Application → Cookies → https://app.slack.com")
	fmt.Fprintln(out, "     copy the value of cookie  d  (starts with xoxd-)")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Paste every xoxc (one per line). Then paste xoxd")
	first, err := readLine(in, out, "xoxc: ")
	if err != nil {
		return err
	}
	if c, d, ok := maybeBlob(first); ok {
		res, err := importws.Save(st, desktop.Found{Xoxc: c, Xoxd: d}, store.SourcePaste)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Saved %s\n", res.Name)
		return nil
	}
	tokens, leftover := takeXoxcLines(first, in)
	xoxd := leftover
	if !strings.HasPrefix(xoxd, "xoxd-") {
		xoxd, err = readLine(in, out, "xoxd: ")
		if err != nil {
			return err
		}
	}
	xoxd = strings.TrimSpace(xoxd)
	if len(tokens) == 0 || !strings.HasPrefix(xoxd, "xoxd-") {
		return fmt.Errorf("those do not look like xoxc / xoxd tokens")
	}
	var n int
	for _, xoxc := range tokens {
		res, err := importws.Save(st, desktop.Found{Xoxc: xoxc, Xoxd: xoxd}, store.SourcePaste)
		if err != nil {
			fmt.Fprintf(out, "  skip: %v\n", err)
			continue
		}
		fmt.Fprintf(out, "  %s %s\n", addedWord(res.Added), res.Name)
		n++
	}
	if n == 0 {
		return fmt.Errorf("Slack rejected these tokens")
	}
	return nil
}

// takeXoxcLines collects xoxc lines until something that is not one arrives:
// the xoxd, a blank line, or EOF. It used to stop as soon as the reader had
// nothing buffered, which is a guess about timing rather than about content -
// a paste split across reads (over ssh, or any paste past the tty's 4KB
// canonical limit) silently lost every token after the first chunk.
func takeXoxcLines(first string, in *bufio.Reader) (tokens []string, leftover string) {
	consider := func(s string) (done bool) {
		s = strings.TrimSpace(s)
		if s == "" {
			return len(tokens) > 0 // a blank line ends a paste already underway
		}
		if strings.HasPrefix(s, "xoxc-") {
			tokens = append(tokens, s)
			return false
		}
		leftover = s
		return true
	}
	if consider(first) {
		return tokens, leftover
	}
	for {
		line, err := in.ReadString('\n')
		if consider(line) {
			return tokens, leftover
		}
		if err != nil {
			return tokens, leftover
		}
	}
}

func importAll(st *store.Store, out io.Writer) (int, error) {
	found, err := importws.Discover()
	if err != nil {
		return 0, err
	}
	var n int
	for _, f := range found {
		res, err := importws.Save(st, f, store.SourceDesktop)
		if err != nil {
			fmt.Fprintf(out, "  skip: %v\n", err)
			continue
		}
		fmt.Fprintf(out, "  %s %s\n", addedWord(res.Added), res.Name)
		n++
	}
	return n, nil
}

func readLine(in *bufio.Reader, out io.Writer, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	s, err := in.ReadString('\n')
	if err != nil && len(strings.TrimSpace(s)) == 0 {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func maybeBlob(s string) (xoxc, xoxd string, ok bool) {
	c, d, err := slackx.DecodeTokenBlob(s)
	if err != nil {
		return "", "", false
	}
	return c, d, true
}

func addedWord(added bool) string {
	if added {
		return "added"
	}
	return "refreshed"
}
