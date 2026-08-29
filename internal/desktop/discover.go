package desktop

import (
	"errors"
	"strings"
)

type Found struct {
	Name   string
	TeamID string
	Xoxc   string
	Xoxd   string
}

func Discover() ([]Found, error) {
	profs := profiles()
	if len(profs) == 0 {
		return nil, errors.New("Slack desktop app data not found (install Slack and sign in)")
	}
	var last error
	for _, p := range profs {
		found, err := discoverProfile(p)
		if err != nil {
			last = err
			continue
		}
		if len(found) > 0 {
			return found, nil
		}
	}
	if last != nil {
		return nil, last
	}
	return nil, errors.New("signed in to Slack, but no workspace tokens were found")
}

func discoverProfile(p Profile) ([]Found, error) {
	xoxd, err := readDCookie(p.Cookies)
	if err != nil {
		return nil, err
	}
	tokens, err := readTokens(p.LevelDB)
	if err != nil {
		return nil, err
	}
	names := workspaceNames(p.RootState)

	seen := map[string]struct{}{}
	var out []Found
	for key, team := range tokens {
		if !looksLikeXoxc(team.Token) {
			continue
		}
		if _, ok := seen[team.Token]; ok {
			continue
		}
		seen[team.Token] = struct{}{}
		teamID := key
		if !strings.HasPrefix(teamID, "T") {
			teamID = ""
		}
		name := team.Name
		if name == "" && teamID != "" {
			name = names[teamID]
		}
		out = append(out, Found{
			Name:   name,
			TeamID: teamID,
			Xoxc:   team.Token,
			Xoxd:   xoxd,
		})
	}
	return out, nil
}
