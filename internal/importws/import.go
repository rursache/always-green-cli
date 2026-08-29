package importws

import (
	"fmt"

	"github.com/rursache/always-green/internal/desktop"
	"github.com/rursache/always-green/internal/slackx"
	"github.com/rursache/always-green/internal/store"
)

type Result struct {
	Name   string
	TeamID string
	Added  bool
}

func Save(st *store.Store, f desktop.Found) (Result, error) {
	auth, err := slackx.AuthTest(f.Xoxc, f.Xoxd)
	if err != nil {
		return Result{}, fmt.Errorf("Slack rejected these tokens: %w", err)
	}
	name := f.Name
	if name == "" {
		name = auth.Team
	}
	if name == "" {
		name = "Workspace"
	}
	profile, _ := slackx.GetUser(f.Xoxc, f.Xoxd, auth.UserID)
	existing, _ := st.Workspaces()
	added := true
	for _, ws := range existing {
		if ws.TeamID == auth.TeamID {
			added = false
			break
		}
	}
	ws := store.Workspace{
		Name:   name,
		TeamID: auth.TeamID,
		UserID: auth.UserID,
		Xoxc:   f.Xoxc,
		Xoxd:   f.Xoxd,
		UserInfo: &store.UserInfo{
			Name: profile.Name, RealName: profile.RealName,
			DisplayName: profile.DisplayName, Email: profile.Email,
		},
	}
	if err := st.SaveWorkspace(ws); err != nil {
		return Result{}, err
	}
	return Result{Name: name, TeamID: auth.TeamID, Added: added}, nil
}

func Discover() ([]desktop.Found, error) {
	return desktop.Discover()
}
