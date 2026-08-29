//go:build linux

package desktop

func safeStoragePassword() ([]byte, error) {
	return []byte("peanuts"), nil
}

func cookieRounds() int { return 1 }
