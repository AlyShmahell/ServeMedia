package watchdog

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type session struct {
	token     string
	expiresAt time.Time
}

type SessionStore struct {
	mu       sync.Mutex
	ttl      time.Duration
	sessions map[string]session
}

func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{
		ttl:      ttl,
		sessions: map[string]session{},
	}
}

type SessionResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (s *SessionStore) Create(now time.Time) SessionResponse {
	token := uuid.NewString()
	exp := now.Add(s.ttl)
	s.mu.Lock()
	s.sessions[token] = session{token: token, expiresAt: exp}
	s.mu.Unlock()
	return SessionResponse{Token: token, ExpiresAt: exp}
}

func (s *SessionStore) Validate(token string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(now)
	sess, ok := s.sessions[token]
	if !ok {
		return false
	}
	if now.After(sess.expiresAt) {
		delete(s.sessions, token)
		return false
	}
	return true
}

func (s *SessionStore) purgeLocked(now time.Time) {
	for tok, sess := range s.sessions {
		if now.After(sess.expiresAt) {
			delete(s.sessions, tok)
		}
	}
}
