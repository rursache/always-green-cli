package ipc

import (
	"bufio"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rursache/always-green-cli/internal/paths"
)

// a unix socket path is capped near 104 bytes, so t.TempDir() is too deep
func withTempHome(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ag")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("HOME", dir)
	if err := paths.EnsureDir(); err != nil {
		t.Fatal(err)
	}
}

// A status reply is unbounded; the old single 8KB read truncated it once
// enough workspaces were configured
func TestSendReadsRepliesLargerThanOneBuffer(t *testing.T) {
	withTempHome(t)
	big := strings.Repeat("x", 200_000)
	ln, err := Listen(func(cmd string) string {
		if cmd != "status" {
			t.Errorf("handler got %q", cmd)
		}
		return big
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	got, err := Send("status")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(big) {
		t.Fatalf("reply truncated: got %d bytes, want %d", len(got), len(big))
	}
}

// The handler used to do one 256-byte read, so a command arriving in pieces
// would be read as a truncated, unknown command
func TestListenReassemblesSplitCommand(t *testing.T) {
	withTempHome(t)
	seen := make(chan string, 1)
	ln, err := Listen(func(cmd string) string {
		seen <- cmd
		return "ok"
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	conn, err := net.Dial("unix", paths.DaemonSock())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	long := "status-" + strings.Repeat("a", 900)
	for _, part := range []string{long[:100], long[100:400], long[400:] + "\n"} {
		if _, err := conn.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case cmd := <-seen:
		if cmd != long {
			t.Fatalf("command reassembled wrong: got %d bytes, want %d", len(cmd), len(long))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler never saw the command")
	}
}

func TestRoundTrip(t *testing.T) {
	withTempHome(t)
	ln, err := Listen(func(cmd string) string { return "echo:" + cmd })
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got, err := Send("ping")
	if err != nil {
		t.Fatal(err)
	}
	if got != "echo:ping" {
		t.Fatalf("got %q", got)
	}
}

func TestReadLineRejectsOversizedMessage(t *testing.T) {
	huge := strings.Repeat("y", maxLine+10) + "\n"
	_, err := readLine(bufio.NewReader(strings.NewReader(huge)))
	if err == nil {
		t.Fatal("expected an oversized message to be rejected")
	}
}
