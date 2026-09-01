package desktop

import (
	"errors"
	"sort"
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
	// keep the first failure: it comes from the most likely profile, and is
	// usually the actionable one (a permission problem rather than "no such
	// file" from a container path the user does not even use)
	var first error
	for _, p := range profs {
		found, err := discoverProfile(p)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if len(found) > 0 {
			return found, nil
		}
	}
	if first != nil {
		return nil, first
	}
	return nil, errors.New("signed in to Slack, but no workspace tokens were found")
}

// isTeamID reports whether a LevelDB key is a Slack-assigned id rather than a
// token we fell back to using as a key; Enterprise Grid orgs are E-prefixed
// orderedKeys sorts identified workspaces ahead of token-keyed fallbacks
func orderedKeys(tokens map[string]teamEntry) []string {
	keys := make([]string, 0, len(tokens))
	for k := range tokens {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := isTeamID(keys[i]), isTeamID(keys[j])
		if a != b {
			return a
		}
		return keys[i] < keys[j]
	})
	return keys
}

func isTeamID(s string) bool {
	if len(s) < 2 || strings.HasPrefix(s, "xoxc-") {
		return false
	}
	return s[0] == 'T' || s[0] == 'E'
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
	// map iteration order is randomised, so walk real team ids first: they
	// carry the name and let RefreshDesktop short-circuit
	for _, key := range orderedKeys(tokens) {
		team := tokens[key]
		if !looksLikeXoxc(team.Token) {
			continue
		}
		if _, ok := seen[team.Token]; ok {
			continue
		}
		seen[team.Token] = struct{}{}
		teamID := key
		if !isTeamID(teamID) {
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
