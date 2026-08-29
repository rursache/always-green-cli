package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

type teamEntry struct {
	Token string `json:"token"`
	Name  string `json:"name"`
	URL   string `json:"url"`
}

type localConfig struct {
	Teams map[string]teamEntry `json:"teams"`
}

var xoxcRe = regexp.MustCompile(`xoxc-[A-Za-z0-9_-]{40,}`)

func readTokens(leveldbDir string) (map[string]teamEntry, error) {
	tmp, err := copyDirFilesTemp(leveldbDir, "ag-leveldb-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	out, err := tokensFromLevelDB(tmp)
	if err == nil && len(out) > 0 {
		return out, nil
	}
	return tokensFromRawScan(tmp)
}

func tokensFromLevelDB(dir string) (map[string]teamEntry, error) {
	db, err := leveldb.OpenFile(dir, &opt.Options{ReadOnly: true, ErrorIfMissing: true})
	if err != nil {
		return nil, err
	}
	defer db.Close()

	out := map[string]teamEntry{}
	iter := db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		key := string(iter.Key())
		val := decodeLSValue(iter.Value())
		if strings.Contains(key, "localConfig_v2") {
			var cfg localConfig
			if json.Unmarshal([]byte(val), &cfg) == nil {
				for id, team := range cfg.Teams {
					if looksLikeXoxc(team.Token) {
						out[id] = team
					}
				}
			}
		}
		for _, tok := range xoxcRe.FindAllString(val, -1) {
			if !looksLikeXoxc(tok) {
				continue
			}
			if _, exists := out[tok]; exists {
				continue
			}
			// token-only fallback, team id filled later via auth.test
			if !hasToken(out, tok) {
				out[tok] = teamEntry{Token: tok}
			}
		}
	}
	return out, iter.Error()
}

func tokensFromRawScan(dir string) (map[string]teamEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]teamEntry{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".ldb") && !strings.HasSuffix(name, ".log") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		for _, tok := range xoxcRe.FindAll(data, -1) {
			s := string(tok)
			if looksLikeXoxc(s) && !hasToken(out, s) {
				out[s] = teamEntry{Token: s}
			}
		}
	}
	return out, nil
}

func decodeLSValue(v []byte) string {
	if len(v) == 0 {
		return ""
	}
	switch v[0] {
	case 0x00:
		return decodeUTF16LE(v[1:])
	case 0x01:
		return string(v[1:])
	default:
		return string(v)
	}
}

func decodeUTF16LE(b []byte) string {
	u16 := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u16 = append(u16, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u16))
}

func looksLikeXoxc(s string) bool {
	return strings.HasPrefix(s, "xoxc-") && len(s) >= 50
}

func hasToken(m map[string]teamEntry, tok string) bool {
	for _, t := range m {
		if t.Token == tok {
			return true
		}
	}
	return false
}
