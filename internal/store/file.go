package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"porkbun-dns/internal/model"
)

type FileStore struct {
	path string
	mu   sync.Mutex
}

type persistedState struct {
	Entries []model.Entry `json:"entries"`
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) List(_ context.Context) ([]model.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.load()
	if err != nil {
		return nil, err
	}

	return append([]model.Entry(nil), state.Entries...), nil
}

func (s *FileStore) Get(ctx context.Context, name string) (model.Entry, bool, error) {
	entries, err := s.List(ctx)
	if err != nil {
		return model.Entry{}, false, err
	}
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true, nil
		}
	}
	return model.Entry{}, false, nil
}

func (s *FileStore) Put(_ context.Context, entry model.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.load()
	if err != nil {
		return err
	}

	entry.UpdatedAt = time.Now().UTC()
	updated := false
	for i := range state.Entries {
		if state.Entries[i].Name == entry.Name {
			state.Entries[i] = entry
			updated = true
			break
		}
	}
	if !updated {
		state.Entries = append(state.Entries, entry)
	}

	sort.Slice(state.Entries, func(i, j int) bool { return state.Entries[i].Name < state.Entries[j].Name })
	return s.save(state)
}

func (s *FileStore) Delete(_ context.Context, name string) (model.Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.load()
	if err != nil {
		return model.Entry{}, false, err
	}

	for i := range state.Entries {
		if state.Entries[i].Name == name {
			entry := state.Entries[i]
			state.Entries = append(state.Entries[:i], state.Entries[i+1:]...)
			if err := s.save(state); err != nil {
				return model.Entry{}, false, err
			}
			return entry, true, nil
		}
	}

	return model.Entry{}, false, nil
}

func (s *FileStore) load() (persistedState, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return persistedState{}, nil
		}
		return persistedState{}, fmt.Errorf("read state file: %w", err)
	}
	if len(data) == 0 {
		return persistedState{}, nil
	}

	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return persistedState{}, fmt.Errorf("decode state file: %w", err)
	}

	sort.Slice(state.Entries, func(i, j int) bool { return state.Entries[i].Name < state.Entries[j].Name })
	return state, nil
}

func (s *FileStore) save(state persistedState) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state file: %w", err)
	}

	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}

	return nil
}
