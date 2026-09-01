package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/rursache/always-green-cli/internal/paths"
)

// maxLine bounds a single request or response so a wedged peer cannot make us
// read forever; status payloads grow with the workspace count, so this is
// generous rather than tight
const maxLine = 4 << 20

func Send(cmd string) (string, error) {
	conn, err := net.DialTimeout("unix", paths.DaemonSock(), 3*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := fmt.Fprintf(conn, "%s\n", cmd); err != nil {
		return "", err
	}
	// read to the newline: a status reply is unbounded and a single fixed
	// buffer silently truncated it once enough workspaces were configured
	r := bufio.NewReaderSize(conn, 64<<10)
	line, err := readLine(r)
	if err != nil {
		return "", err
	}
	return line, nil
}

func Listen(handle func(cmd string) string) (net.Listener, error) {
	_ = os.Remove(paths.DaemonSock())
	ln, err := net.Listen("unix", paths.DaemonSock())
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(paths.DaemonSock(), 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(10 * time.Second))
				r := bufio.NewReader(c)
				cmd, err := readLine(r)
				if err != nil {
					return
				}
				_, _ = c.Write([]byte(handle(cmd) + "\n"))
			}(conn)
		}
	}()
	return ln, nil
}

// readLine reads until newline, tolerating a message split across reads
func readLine(r *bufio.Reader) (string, error) {
	var sb strings.Builder
	for {
		chunk, more, err := r.ReadLine()
		if err != nil {
			if sb.Len() > 0 {
				return strings.TrimSpace(sb.String()), nil
			}
			return "", err
		}
		if sb.Len()+len(chunk) > maxLine {
			return "", fmt.Errorf("ipc message exceeds %d bytes", maxLine)
		}
		sb.Write(chunk)
		if !more {
			return strings.TrimSpace(sb.String()), nil
		}
	}
}

func Encode(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"encode"}`
	}
	return string(b)
}
