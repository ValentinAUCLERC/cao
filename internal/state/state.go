package state

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/valentin/cao/internal/fsutil"
)

type Entry struct {
	Path      string    `json:"path"`
	Kind      string    `json:"kind"`
	Workspace string    `json:"workspace,omitempty"`
	Owner     string    `json:"owner,omitempty"`
	Source    string    `json:"source"`
	Hash      string    `json:"hash"`
	Mode      string    `json:"mode"`
	Sensitive bool      `json:"sensitive"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type State struct {
	Version           int              `json:"version"`
	AppliedWorkspaces []string         `json:"appliedWorkspaces,omitempty"`
	UpdatedAt         time.Time        `json:"updatedAt"`
	Entries           map[string]Entry `json:"entries"`
}

type entryCompat struct {
	Entry
	LegacyModule string `json:"module,omitempty"`
}

type stateCompat struct {
	Version           int                        `json:"version"`
	AppliedWorkspaces []string                   `json:"appliedWorkspaces,omitempty"`
	UpdatedAt         time.Time                  `json:"updatedAt"`
	Entries           map[string]json.RawMessage `json:"entries"`
}

func New() *State {
	return &State{
		Version: 1,
		Entries: map[string]Entry{},
	}
}

func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}

	var compat stateCompat
	if err := json.Unmarshal(data, &compat); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}

	current := &State{
		Version:           compat.Version,
		AppliedWorkspaces: compat.AppliedWorkspaces,
		UpdatedAt:         compat.UpdatedAt,
		Entries:           map[string]Entry{},
	}
	if current.Version == 0 {
		current.Version = 1
	}
	for path, raw := range compat.Entries {
		var entry entryCompat
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, fmt.Errorf("decode state entry %s: %w", path, err)
		}
		if entry.Owner == "" && entry.LegacyModule != "" {
			entry.Owner = entry.LegacyModule
		}
		current.Entries[path] = entry.Entry
	}
	return current, nil
}

func Save(path string, current *State) error {
	current.Version = 1
	current.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	return fsutil.WriteFileAtomic(path, append(data, '\n'), 0o600)
}

func (e Entry) OwnerLabel() string {
	return e.Owner
}
