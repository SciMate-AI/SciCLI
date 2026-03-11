package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SciMate-AI/scicli/internal/config"
)

type RunRef struct {
	Server    string    `json:"server"`
	ProjectID string    `json:"project_id"`
	RunID     string    `json:"run_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

type State struct {
	LastRun *RunRef `json:"last_run,omitempty"`
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
		path: filepath.Join(dataDir, "state.json"),
	}, nil
}

func (s *Store) Load() (*State, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil
		}
		return nil, fmt.Errorf("failed to read state: %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("failed to decode state: %w", err)
	}
	return &st, nil
}

func (s *Store) Save(state *State) error {
	if state == nil {
		state = &State{}
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode state: %w", err)
	}
	if err := os.WriteFile(s.path, payload, 0o600); err != nil {
		return fmt.Errorf("failed to write state: %w", err)
	}
	return nil
}
