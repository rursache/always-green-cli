//go:build linux

package desktop

func safeStoragePasswords() ([][]byte, error) {
	return [][]byte{[]byte("peanuts")}, nil
}

func cookieRounds() int { return 1 }
