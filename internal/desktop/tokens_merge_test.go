package desktop

import (
	"strings"
	"testing"
)

const longTok = "xoxc-1111111111111-2222222222222-3333333333333-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// A token that also appears loose elsewhere in LevelDB must not get a second,
// token-keyed entry that can shadow the real team entry
func TestMergeLooseTokensKeepsTeamEntry(t *testing.T) {
	out := map[string]teamEntry{
		"T0123": {Token: longTok, Name: "acme"},
	}
	mergeLooseTokens(out, []string{longTok})
	if len(out) != 1 {
		t.Fatalf("token already claimed by a team should not be added again: %v", out)
	}
	if out["T0123"].Name != "acme" {
		t.Fatal("team entry was overwritten")
	}
}

func TestMergeLooseTokensAddsUnclaimed(t *testing.T) {
	other := strings.Replace(longTok, "1111111111111", "9999999999999", 1)
	out := map[string]teamEntry{"T0123": {Token: longTok, Name: "acme"}}
	mergeLooseTokens(out, []string{other, "xoxc-short", other})
	if len(out) != 2 {
		t.Fatalf("expected the unclaimed token added once, got %v", out)
	}
	if out[other].Token != other {
		t.Fatal("unclaimed token missing")
	}
}

// Enterprise Grid orgs are E-prefixed; the old T-only check discarded a real
// id that localConfig_v2 had already given us
func TestIsTeamID(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"T0123ABCD", true},
		{"E0123ABCD", true},
		{longTok, false},
		{"xoxc-anything", false},
		{"", false},
		{"T", false},
		{"localConfig_v2", false},
	} {
		if got := isTeamID(tc.in); got != tc.want {
			t.Errorf("isTeamID(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// Ordering must not depend on Go's randomised map iteration
func TestOrderedKeysPutsTeamIDsFirst(t *testing.T) {
	tokens := map[string]teamEntry{
		longTok:   {Token: longTok},
		"T0123":   {Token: "a"},
		"E0999":   {Token: "b"},
		"zzloose": {Token: "c"},
	}
	for i := 0; i < 50; i++ {
		keys := orderedKeys(tokens)
		if len(keys) != 4 {
			t.Fatalf("got %d keys", len(keys))
		}
		if !isTeamID(keys[0]) || !isTeamID(keys[1]) {
			t.Fatalf("team ids must sort first, got %v", keys)
		}
		if keys[0] != "E0999" || keys[1] != "T0123" {
			t.Fatalf("order is not stable across runs: %v", keys)
		}
	}
}
