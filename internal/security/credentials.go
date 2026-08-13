package security

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Credentials stores agent identity after registration.
type Credentials struct {
	AgentID     string `json:"agentId"`
	AccessToken string `json:"accessToken"`
}

// Store persists credentials under the data directory.
type Store struct {
	path string
}

// NewStore creates a credential store at dataDir/credentials.json.
func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return &Store{path: filepath.Join(dataDir, "credentials.json")}, nil
}

// Load returns stored credentials, or nil if none exist.
func (s *Store) Load() (*Credentials, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	if creds.AgentID == "" || creds.AccessToken == "" {
		return nil, nil
	}
	return &creds, nil
}

// Save writes credentials with restrictive permissions.
func (s *Store) Save(creds *Credentials) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}
