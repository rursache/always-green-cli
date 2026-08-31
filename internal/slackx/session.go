package slackx

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsURL          = "wss://wss-primary.slack.com/"
	pingEvery      = 30 * time.Second
	tickleEvery    = 5 * time.Minute
	presenceEvery  = 60 * time.Second
	maxConnectTry  = 10
	baseBackoff    = 5 * time.Second
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

	var conn *websocket.Conn
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

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

	for attempt := 1; attempt <= maxConnectTry; attempt++ {
		if ctx.Err() != nil || !s.running.Load() {
			return
		}
		if connect() {
			break
		}
		if !s.tokenValid {
			return
		}
		if attempt == maxConnectTry {
			s.setState("error", false)
			return
		}
		s.setState("reconnecting", false)
		select {
		case <-ctx.Done():
			return
		case <-time.After(baseBackoff * time.Duration(attempt)):
		}
	}

	pingT := time.NewTicker(pingEvery)
	tickleT := time.NewTicker(tickleEvery)
	presT := time.NewTicker(presenceEvery)
	defer pingT.Stop()
	defer tickleT.Stop()
	defer presT.Stop()

	incoming := make(chan []byte, 8)
	readErr := make(chan error, 1)
	startReader := func() {
		go func(c *websocket.Conn) {
			for {
				_, data, err := c.ReadMessage()
				if err != nil {
					readErr <- err
					return
				}
				select {
				case incoming <- data:
				default:
				}
			}
		}(conn)
	}
	startReader()

	for s.running.Load() {
		select {
		case <-ctx.Done():
			return
		case err := <-readErr:
			log.Printf("[%s] websocket closed: %v", s.Name, err)
			s.setState("disconnected", false)
			if rejected, _ := s.tokensRejected(); rejected {
				s.markDead()
				return
			}
			if !connect() {
				if !s.tokenValid {
					return
				}
				s.setState("error", false)
				return
			}
			startReader()
		case data := <-incoming:
			s.handleMessage(data)
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
			s.checkPresence(ctx, connect, &conn, &startReader)
			if !s.tokenValid {
				return
			}
		}
	}
}

func (s *Session) Stop() {
	s.running.Store(false)
}

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
	hdr.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	hdr.Set("Origin", "https://app.slack.com")

	dialer := websocket.Dialer{HandshakeTimeout: 8 * time.Second}
	conn, resp, err := dialer.Dial(u.String(), hdr)
	if err != nil {
		if resp != nil && resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			return nil, fmt.Errorf("ws handshake %d: %w", resp.StatusCode, err)
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

func (s *Session) checkPresence(ctx context.Context, connect func() bool, conn **websocket.Conn, startReader *func()) {
	pres, err := GetPresence(s.Xoxc, s.Xoxd, s.UserID)
	if err != nil {
		if api, ok := err.(APIError); ok && TokenDead(api.Code) {
			s.markDead()
		}
		return
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
			if connect() {
				(*startReader)()
			}
		}
	}
	_ = ctx
	_ = conn
}

func (s *Session) tokensRejected() (bool, error) {
	_, err := AuthTest(s.Xoxc, s.Xoxd)
	if err == nil {
		return false, nil
	}
	if api, ok := err.(APIError); ok && TokenDead(api.Code) {
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
	s.running.Store(false)
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
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, " 401") || strings.Contains(s, " 403")
}
