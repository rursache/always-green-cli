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

func Save(st *store.Store, f desktop.Found, source string) (Result, error) {
	auth, err := slackx.AuthTest(f.Xoxc, f.Xoxd)
	if err != nil {
		return Result{}, fmt.Errorf("Slack rejected these tokens: %w", err)
	}
	return save(st, f, auth, source)
}

func save(st *store.Store, f desktop.Found, auth slackx.Auth, source string) (Result, error) {
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
		Source: source,
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

// RefreshDesktop re-reads one workspace's tokens from the Slack desktop app.
// Only the entry Slack itself confirms belongs to teamID is saved, so a stale
// or unrelated profile cannot overwrite the wrong workspace.
func RefreshDesktop(st *store.Store, teamID string) error {
	found, err := desktop.Discover()
	if err != nil {
		return err
	}
	for _, f := range found {
		if f.TeamID != "" && f.TeamID != teamID {
			continue
		}
		auth, err := slackx.AuthTest(f.Xoxc, f.Xoxd)
		if err != nil || auth.TeamID != teamID {
			continue
		}
		_, err = save(st, f, auth, store.SourceDesktop)
		return err
	}
	return fmt.Errorf("the Slack app has no working tokens for this workspace")
}

func Discover() ([]desktop.Found, error) {
	return desktop.Discover()
}
