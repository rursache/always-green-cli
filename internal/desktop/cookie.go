package desktop

import (
	"database/sql"
	"errors"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

func readDCookie(dbPath string) (string, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return "", err
	}
	tmp, err := copyFileTemp(dbPath, "ag-cookies-*.db")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp)

	db, err := sql.Open("sqlite", tmp)
	if err != nil {
		return "", err
	}
	defer db.Close()

	var plain string
	var enc []byte
	err = db.QueryRow(`SELECT IFNULL(value,''), encrypted_value FROM cookies WHERE name='d' AND host_key LIKE '%slack.com%' LIMIT 1`).Scan(&plain, &enc)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("no Slack d cookie in the desktop app")
		}
		return "", err
	}
	if strings.HasPrefix(plain, "xoxd-") {
		return plain, nil
	}
	if len(enc) == 0 {
		return "", errors.New("Slack d cookie is empty")
	}
	password, err := safeStoragePassword()
	if err != nil {
		return "", err
	}
	dec, err := decryptCookie(enc, password, cookieRounds())
	if err != nil {
		return "", err
	}
	val := strings.TrimSpace(string(dec))
	if !strings.HasPrefix(val, "xoxd-") {
		return "", errors.New("decrypted cookie is not an xoxd token")
	}
	return val, nil
}
