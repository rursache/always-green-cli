//go:build !darwin && !linux

package desktop

import "errors"

func safeStoragePasswords() ([][]byte, error) {
	return nil, errors.New("Slack desktop import is not supported on this OS")
}

func cookieRounds() int { return 1003 }
