package slackx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// wsURL is a var so tests can point a session at a local server
var wsURL = "wss://wss-primary.slack.com/"

// tunable so tests can drive the loop without waiting on wall-clock minutes
var (
	pingEvery     = 30 * time.Second
	tickleEvery   = 5 * time.Minute
	presenceEvery = 60 * time.Second
	baseBackoff   = 5 * time.Second
)

const (
	maxConnectTry  = 10
	awayReconnectN = 5
)

type Snapshot struct {
	Name       string
	TeamID     string
	Status     string
	Connected  bool
	TokenValid bool
	Uptime     time.Duration
	LastActive time.Time
	Connects   int
}

type Session struct {
	Name   string
	TeamID string
	UserID string
	Xoxc   string
	Xoxd   string

	// OnTokenDead fires once, from the session goroutine, when Slack has
	// confirmed via auth.test that these tokens are no longer usable
	OnTokenDead func(teamID, name string)

	mu         sync.Mutex
	status     string
	connected  bool
	tokenValid bool
	started    time.Time
	lastActive time.Time
	connects   int
	msgID      int
	awayHits   int

	running atomic.Bool
}

// readEvent carries a read from one websocket; gen identifies which connection
// produced it: closing a connection makes its reader report an error, and
// without the generation that error looks exactly like the replacement
// connection dying, which used to cascade into an endless reconnect loop
type readEvent struct {
	gen  int
	data []byte
	err  error
}

func NewSession(name, teamID, userID, xoxc, xoxd string) *Session {
	return &Session{
		Name:       name,
		TeamID:     teamID,
		UserID:     userID,
		Xoxc:       xoxc,
		Xoxd:       xoxd,
		status:     "disconnected",
		tokenValid: true,
	}
}

func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	up := time.Duration(0)
	if !s.started.IsZero() {
		up = time.Since(s.started)
	}
	return Snapshot{
		Name:       s.Name,
		TeamID:     s.TeamID,
		Status:     s.status,
		Connected:  s.connected,
		TokenValid: s.tokenValid,
		Uptime:     up,
		LastActive: s.lastActive,
		Connects:   s.connects,
	}
}

func (s *Session) Running() bool { return s.running.Load() }

func (s *Session) Run(ctx context.Context) {
	s.running.Store(true)
	defer s.running.Store(false)
	defer s.finish()
	s.setState("connecting", false)

	// closing done releases any reader still blocked handing us an event
	done := make(chan struct{})
	defer close(done)
	events := make(chan readEvent, 8)

	var conn *websocket.Conn
	gen := 0
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	// connect owns the reader lifecycle: every successful dial bumps the
	// generation and starts exactly one reader for that connection
	connect := func() bool {
		if conn != nil {
			_ = conn.Close()
			conn = nil
		}
		c, err := s.dial()
		if err != nil {
			log.Printf("[%s] connect failed: %v", s.Name, err)
			if isAuthFail(err) {
				s.markDead()
				return false
			}
			if rejected, _ := s.tokensRejected(); rejected {
				s.markDead()
				return false
			}
			return false
		}
		conn = c
		gen++
		go readLoop(c, gen, events, done)
		s.mu.Lock()
		s.connected = true
		s.status = "active"
		s.connects++
		s.lastActive = time.Now()
		if s.started.IsZero() {
			s.started = time.Now()
		}
		s.mu.Unlock()
		log.Printf("[%s] connected", s.Name)
		return true
	}

	// used for the first connection and for every later reconnect, so a
	// transient blip mid-session gets the same backoff as one at startup
	connectWithRetry := func() bool {
		for attempt := 1; attempt <= maxConnectTry; attempt++ {
			if ctx.Err() != nil || !s.running.Load() {
				return false
			}
			if connect() {
				return true
			}
			if !s.tokenValid || attempt == maxConnectTry {
				return false
			}
			s.setState("reconnecting", false)
			select {
			case <-ctx.Done():
				return false
			case <-time.After(baseBackoff * time.Duration(attempt)):
			}
		}
		return false
	}

	if !connectWithRetry() {
		if s.tokenValid {
			s.setState("error", false)
		}
		return
	}

	pingT := time.NewTicker(pingEvery)
	tickleT := time.NewTicker(tickleEvery)
	presT := time.NewTicker(presenceEvery)
	defer pingT.Stop()
	defer tickleT.Stop()
	defer presT.Stop()

	for s.running.Load() {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			if ev.gen != gen {
				continue // a reader for a connection we already replaced
			}
			if ev.err != nil {
				log.Printf("[%s] websocket closed: %v", s.Name, ev.err)
				s.setState("disconnected", false)
				if rejected, _ := s.tokensRejected(); rejected {
					s.markDead()
					return
				}
				if !connectWithRetry() {
					if s.tokenValid {
						s.setState("error", false)
					}
					return
				}
				continue
			}
			s.handleMessage(ev.data)
		case <-pingT.C:
			if conn != nil {
				_ = s.sendJSON(conn, map[string]any{"id": s.nextID(), "type": "ping"})
			}
		case <-tickleT.C:
			if conn != nil {
				if err := s.sendJSON(conn, map[string]any{
					"type": "tickle", "reason": "mousedown", "id": s.nextID(),
				}); err == nil {
					log.Printf("[%s] sent tickle", s.Name)
				}
			}
		case <-presT.C:
			if !s.checkPresence(connectWithRetry) {
				if s.tokenValid {
					s.setState("error", false)
				}
				return
			}
		}
	}
}

func readLoop(c *websocket.Conn, gen int, events chan<- readEvent, done <-chan struct{}) {
	for {
		_, data, err := c.ReadMessage()
		select {
		case events <- readEvent{gen: gen, data: data, err: err}:
		case <-done:
			return
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) Stop() {
	s.running.Store(false)
}

// handshakeError keeps the HTTP status Slack rejected the upgrade with, so
// auth failures are classified on the real status rather than on error text
type handshakeError struct {
	Status int
	err    error
}

func (e *handshakeError) Error() string {
	return fmt.Sprintf("ws handshake %d: %v", e.Status, e.err)
}

func (e *handshakeError) Unwrap() error { return e.err }

func (s *Session) dial() (*websocket.Conn, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("token", s.Xoxc)
	u.RawQuery = q.Encode()

	hdr := http.Header{}
	hdr.Set("Cookie", "d="+s.Xoxd)
	hdr.Set("User-Agent", userAgent)
	hdr.Set("Origin", "https://app.slack.com")

	dialer := websocket.Dialer{HandshakeTimeout: 8 * time.Second}
	conn, resp, err := dialer.Dial(u.String(), hdr)
	if err != nil {
		if resp != nil {
			return nil, &handshakeError{Status: resp.StatusCode, err: err}
		}
		return nil, err
	}
	return conn, nil
}

func (s *Session) sendJSON(conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return err
	}
	s.mu.Lock()
	s.lastActive = time.Now()
	s.mu.Unlock()
	return nil
}

func (s *Session) handleMessage(data []byte) {
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	switch msg["type"] {
	case "hello":
		log.Printf("[%s] hello from Slack", s.Name)
	case "presence_change":
		if user, _ := msg["user"].(string); user == s.UserID {
			if p, _ := msg["presence"].(string); p != "" {
				s.mu.Lock()
				s.status = p
				s.mu.Unlock()
				log.Printf("[%s] presence changed to %s", s.Name, p)
			}
		}
	}
}

// checkPresence reports false when the session can no longer continue, either
// because the tokens died or because a presence-triggered reconnect failed
func (s *Session) checkPresence(reconnect func() bool) bool {
	pres, err := GetPresence(s.Xoxc, s.Xoxd, s.UserID)
	if err != nil {
		var api APIError
		if errors.As(err, &api) && TokenDead(api.Code) {
			s.markDead()
			return false
		}
		log.Printf("[%s] presence check failed: %v", s.Name, err)
		return true
	}
	kind := classify(pres)
	log.Printf("[%s] %s", s.Name, kind)
	switch kind {
	case "active":
		s.mu.Lock()
		s.awayHits = 0
		s.status = "active"
		s.mu.Unlock()
	case "manual_away":
		s.mu.Lock()
		s.awayHits = 0
		s.status = "manual_away"
		s.mu.Unlock()
	case "away", "auto_away":
		s.mu.Lock()
		s.awayHits++
		hits := s.awayHits
		s.status = kind
		s.mu.Unlock()
		if shouldReconnect(hits) {
			log.Printf("[%s] reconnecting to reset presence", s.Name)
			if !reconnect() {
				return false
			}
		}
	}
	return true
}

func (s *Session) tokensRejected() (bool, error) {
	_, err := AuthTest(s.Xoxc, s.Xoxd)
	if err == nil {
		return false, nil
	}
	var api APIError
	if errors.As(err, &api) && TokenDead(api.Code) {
		return true, err
	}
	return false, err
}

func (s *Session) markDead() {
	s.mu.Lock()
	already := !s.tokenValid
	s.tokenValid = false
	s.status = "invalid_token"
	s.connected = false
	cb := s.OnTokenDead
	s.mu.Unlock()
	// the session stays Running until the callback returns, so a reconciler
	// that sees invalid_token on a running session knows the death is still
	// being handled and does not respawn it with the same dead tokens
	defer s.running.Store(false)
	if already {
		return
	}
	log.Printf("[%s] Slack rejected the tokens", s.Name)
	if cb != nil {
		cb(s.TeamID, s.Name)
	}
}

// finish records the terminal state, leaving invalid_token intact so the
// reason a session ended is not lost behind a generic "stopped"
func (s *Session) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = false
	if s.status == "invalid_token" {
		return
	}
	s.status = "stopped"
}

func (s *Session) setState(status string, connected bool) {
	s.mu.Lock()
	s.status = status
	s.connected = connected
	s.mu.Unlock()
}

func (s *Session) nextID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgID++
	return s.msgID
}

func classify(p Presence) string {
	if p.Presence == "active" {
		return "active"
	}
	if p.Presence == "away" {
		if p.ManualAway {
			return "manual_away"
		}
		if p.AutoAway {
			return "auto_away"
		}
		return "away"
	}
	return "other"
}

func shouldReconnect(hits int) bool {
	if hits <= awayReconnectN {
		return true
	}
	return hits%awayReconnectN == 0
}

func isAuthFail(err error) bool {
	var he *handshakeError
	if !errors.As(err, &he) {
		return false
	}
	return he.Status == http.StatusUnauthorized || he.Status == http.StatusForbidden
}
