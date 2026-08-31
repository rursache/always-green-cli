package desktop

import (
	"os"
	"path/filepath"
	"runtime"
)

type Profile struct {
	Root      string
	Cookies   string
	LevelDB   string
	RootState string
}

func profiles() []Profile {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var roots []string
	switch runtime.GOOS {
	case "darwin":
		roots = []string{
			filepath.Join(home, "Library", "Application Support", "Slack"),
			filepath.Join(home, "Library", "Containers", "com.tinyspeck.slackmacgap",
				"Data", "Library", "Application Support", "Slack"),
		}
	case "linux":
		roots = []string{
			filepath.Join(home, ".config", "Slack"),
			filepath.Join(home, ".var", "app", "com.slack.Slack", "config", "Slack"),
		}
	default:
		return nil
	}
	var out []Profile
	for _, root := range roots {
		ldb := filepath.Join(root, "Local Storage", "leveldb")
		if st, err := os.Stat(ldb); err != nil || !st.IsDir() {
			continue
		}
		out = append(out, Profile{
			Root:      root,
			Cookies:   filepath.Join(root, "Cookies"),
			LevelDB:   ldb,
			RootState: filepath.Join(root, "storage", "root-state.json"),
		})
	}
	return out
}
