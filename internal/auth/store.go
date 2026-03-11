package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/config"
)

type Session struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Email        string    `json:"email,omitempty"`
}

type Store struct {
	path string
}

func NewStore() (*Store, error) {
	dataDir := strings.TrimSpace(config.DataDirectory())
	if dataDir == "" {
		return nil, fmt.Errorf("data directory is not configured")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}
	return &Store{
		path: filepath.Join(dataDir, "auth.json"),
	}, nil
}

func (s *Store) Load() (*Session, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read auth store: %w", err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to decode auth store: %w", err)
	}
	if strings.TrimSpace(session.AccessToken) == "" {
		return nil, nil
	}
	return &session, nil
}

func (s *Store) Save(session *Session) error {
	if session == nil {
		return s.Clear()
	}
	payload, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode auth store: %w", err)
	}
	if err := os.WriteFile(s.path, payload, 0o600); err != nil {
		return fmt.Errorf("failed to write auth store: %w", err)
	}
	return nil
}

func (s *Store) Clear() error {
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear auth store: %w", err)
	}
	return nil
}
