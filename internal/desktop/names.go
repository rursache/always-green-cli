package desktop

import (
	"encoding/json"
	"os"
)

func workspaceNames(rootState string) map[string]string {
	raw, err := os.ReadFile(rootState)
	if err != nil {
		return nil
	}
	var doc struct {
		Workspaces map[string]struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Domain string `json:"domain"`
		} `json:"workspaces"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	out := map[string]string{}
	for id, ws := range doc.Workspaces {
		name := ws.Name
		if name == "" {
			name = ws.Domain
		}
		if name != "" {
			out[id] = name
		}
	}
	return out
}
