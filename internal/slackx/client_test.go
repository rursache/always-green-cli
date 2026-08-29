package slackx

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestDecodeTokenBlob(t *testing.T) {
	raw, _ := json.Marshal(map[string]string{"xoxc": "xoxc-1", "xoxd": "xoxd-2"})
	blob := base64.StdEncoding.EncodeToString(raw)
	c, d, err := DecodeTokenBlob(blob)
	if err != nil {
		t.Fatal(err)
	}
	if c != "xoxc-1" || d != "xoxd-2" {
		t.Fatalf("got %s %s", c, d)
	}
}

func TestTokenDead(t *testing.T) {
	if !TokenDead("invalid_auth") {
		t.Fatal("expected invalid_auth to be dead")
	}
	if TokenDead("ratelimited") {
		t.Fatal("ratelimited is transient")
	}
}

func TestClassify(t *testing.T) {
	if classify(Presence{Presence: "active"}) != "active" {
		t.Fatal("active")
	}
	if classify(Presence{Presence: "away", ManualAway: true}) != "manual_away" {
		t.Fatal("manual")
	}
	if classify(Presence{Presence: "away", AutoAway: true}) != "auto_away" {
		t.Fatal("auto")
	}
	if classify(Presence{Presence: "away"}) != "away" {
		t.Fatal("away")
	}
}

func TestShouldReconnect(t *testing.T) {
	for i := 1; i <= 5; i++ {
		if !shouldReconnect(i) {
			t.Fatalf("hit %d should reconnect", i)
		}
	}
	if shouldReconnect(6) {
		t.Fatal("hit 6 should wait")
	}
	if !shouldReconnect(10) {
		t.Fatal("hit 10 should reconnect")
	}
}
