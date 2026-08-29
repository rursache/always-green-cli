package desktop

import "testing"

func TestReadLocalSlackLevelDB(t *testing.T) {
	profs := profiles()
	if len(profs) == 0 {
		t.Skip("Slack desktop data not present")
	}
	tokens, err := readTokens(profs[0].LevelDB)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, team := range tokens {
		if looksLikeXoxc(team.Token) {
			n++
		}
	}
	if n == 0 {
		t.Fatal("expected at least one xoxc token in Slack LevelDB")
	}
}

func TestReadLocalWorkspaceNames(t *testing.T) {
	profs := profiles()
	if len(profs) == 0 {
		t.Skip("Slack desktop data not present")
	}
	names := workspaceNames(profs[0].RootState)
	if len(names) == 0 {
		t.Fatal("expected workspace names in root-state.json")
	}
}
