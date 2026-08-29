package bootstrap

import (
	"bufio"
	"strings"
	"testing"
)

func TestTakeXoxcLinesMultiple(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("xoxc-aaa\nxoxc-bbb\nxoxc-ccc\nxoxd-cookie\n"))
	first, err := in.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	tokens, leftover := takeXoxcLines(strings.TrimSpace(first), in)
	if len(tokens) != 3 {
		t.Fatalf("got %d tokens %v", len(tokens), tokens)
	}
	if tokens[0] != "xoxc-aaa" || tokens[1] != "xoxc-bbb" || tokens[2] != "xoxc-ccc" {
		t.Fatalf("tokens %v", tokens)
	}
	if leftover != "xoxd-cookie" {
		t.Fatalf("leftover %q", leftover)
	}
}

func TestTakeXoxcLinesThenPrompt(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("xoxc-aaa\nxoxc-bbb\n"))
	first, err := in.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	tokens, leftover := takeXoxcLines(strings.TrimSpace(first), in)
	if len(tokens) != 2 {
		t.Fatalf("got %v", tokens)
	}
	if leftover != "" {
		t.Fatalf("leftover %q", leftover)
	}
}
