package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"always-green/internal/paths"
)

func Send(cmd string) (string, error) {
	conn, err := net.DialTimeout("unix", paths.DaemonSock(), 3*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(conn, "%s\n", cmd); err != nil {
		return "", err
	}
	buf := make([]byte, 8192)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf[:n])), nil
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
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				buf := make([]byte, 256)
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				cmd := strings.TrimSpace(string(buf[:n]))
				_, _ = c.Write([]byte(handle(cmd) + "\n"))
			}(conn)
		}
	}()
	return ln, nil
}

func Encode(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"encode"}`
	}
	return string(b)
}
