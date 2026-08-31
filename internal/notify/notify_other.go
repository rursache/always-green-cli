//go:build !darwin && !linux

package notify

func send(title, body string) {}
