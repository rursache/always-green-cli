package bootstrap

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"
)

// A real paste does not arrive in one read: bracketed paste over ssh, and any
// paste past the tty's 4KB canonical limit, land in chunks with a gap between
// them. Reading only what was already buffered dropped everything after the
// first chunk.
func TestTakeXoxcLinesSurvivesChunkedPaste(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	chunks := []string{"xoxc-aaa\n", "xoxc-bbb\n", "xoxc-ccc\n", "xoxd-cookie\n"}
	go func() {
		defer w.Close()
		for _, c := range chunks {
			time.Sleep(25 * time.Millisecond)
			if _, err := w.WriteString(c); err != nil {
				return
			}
		}
	}()

	in := bufio.NewReader(r)
	first, err := in.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		tokens   []string
		leftover string
	}
	done := make(chan result, 1)
	go func() {
		tokens, leftover := takeXoxcLines(strings.TrimSpace(first), in)
		done <- result{tokens, leftover}
	}()

	select {
	case got := <-done:
		if len(got.tokens) != 3 {
			t.Fatalf("chunked paste lost tokens: got %v, want 3", got.tokens)
		}
		if got.leftover != "xoxd-cookie" {
			t.Fatalf("leftover = %q, want the xoxd line", got.leftover)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("takeXoxcLines never returned")
	}
}

// A blank line is how a user signals the paste is finished
func TestTakeXoxcLinesStopsOnBlankLine(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("xoxc-bbb\n\nignored\n"))
	tokens, leftover := takeXoxcLines("xoxc-aaa", in)
	if len(tokens) != 2 {
		t.Fatalf("got %v", tokens)
	}
	if leftover != "" {
		t.Fatalf("leftover %q, want empty", leftover)
	}
}

func TestTakeXoxcLinesStopsAtEOF(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("xoxc-bbb\nxoxc-ccc"))
	tokens, leftover := takeXoxcLines("xoxc-aaa", in)
	if len(tokens) != 3 {
		t.Fatalf("an unterminated final line should still count: got %v", tokens)
	}
	if leftover != "" {
		t.Fatalf("leftover %q", leftover)
	}
}

// The first-run "1) Slack desktop app" branch built a second bufio.Reader over
// stdin, discarding anything the first had already pulled in
func TestChromeFallbackKeepsBufferedInput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// everything arrives in one write, exactly as a pre-staged paste would
	if _, err := w.WriteString("1\nxoxc-aaa\nxoxd-cookie\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	in := bufio.NewReader(r)
	choice, err := in.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(choice) != "1" {
		t.Fatalf("choice %q", choice)
	}

	// the same reader must still hold the rest of the paste
	first, err := in.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	tokens, leftover := takeXoxcLines(strings.TrimSpace(first), in)
	if len(tokens) != 1 || tokens[0] != "xoxc-aaa" {
		t.Fatalf("tokens %v", tokens)
	}
	if leftover != "xoxd-cookie" {
		t.Fatalf("leftover %q", leftover)
	}
}
