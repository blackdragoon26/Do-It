package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	errBadInput = errors.New("bad input")
	errNotFound = errors.New("not found")
)

type Attachment struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Type      string    `json:"type"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

type Task struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Notes       string       `json:"notes"`
	ParentID    string       `json:"parentId,omitempty"`
	Done        bool         `json:"done"`
	Attachments []Attachment `json:"attachments,omitempty"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

type Snapshot struct {
	Tasks     []Task    `json:"tasks"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type TaskPatch struct {
	Title    *string
	Notes    *string
	ParentID *string
	Done     *bool
}

type Store struct {
	mu        sync.RWMutex
	path      string
	tasks     map[string]Task
	updatedAt time.Time
}

type persistedState struct {
	Tasks     []Task    `json:"tasks"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func NewStore(path string) (*Store, error) {
	store := &Store{
		path:      path,
		tasks:     make(map[string]Task),
		updatedAt: time.Now().UTC(),
	}

	if err := store.load(); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := store.seedDefaults(); err != nil {
			return nil, err
		}
		if err := store.persistLocked(); err != nil {
			return nil, err
		}
	}

	return store, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) Task(id string) (Task, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Task{}, fmt.Errorf("%w: id is required", errBadInput)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[id]
	if !ok {
		return Task{}, errNotFound
	}
	if len(task.Attachments) > 0 {
		task.Attachments = append([]Attachment(nil), task.Attachments...)
	}
	return task, nil
}

func (s *Store) AddTask(title, notes, parentID string, attachments []Attachment) (Snapshot, Task, error) {
	title = strings.TrimSpace(title)
	notes = strings.TrimSpace(notes)
	parentID = strings.TrimSpace(parentID)
	if title == "" {
		return Snapshot{}, Task{}, fmt.Errorf("%w: title is required", errBadInput)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if parentID != "" {
		if _, ok := s.tasks[parentID]; !ok {
			return Snapshot{}, Task{}, fmt.Errorf("%w: parent task does not exist", errBadInput)
		}
	}

	now := time.Now().UTC()
	id, err := newID("task")
	if err != nil {
		return Snapshot{}, Task{}, err
	}
	task := Task{
		ID:          id,
		Title:       title,
		Notes:       notes,
		ParentID:    parentID,
		Attachments: attachments,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.tasks[task.ID] = task
	s.updatedAt = now
	if err := s.persistLocked(); err != nil {
		return Snapshot{}, Task{}, err
	}

	return s.snapshotLocked(), task, nil
}

func (s *Store) PatchTask(id string, patch TaskPatch) (Snapshot, Task, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Snapshot{}, Task{}, fmt.Errorf("%w: id is required", errBadInput)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return Snapshot{}, Task{}, errNotFound
	}

	if patch.Title != nil {
		title := strings.TrimSpace(*patch.Title)
		if title == "" {
			return Snapshot{}, Task{}, fmt.Errorf("%w: title is required", errBadInput)
		}
		task.Title = title
	}
	if patch.Notes != nil {
		task.Notes = strings.TrimSpace(*patch.Notes)
	}
	if patch.Done != nil {
		task.Done = *patch.Done
	}
	if patch.ParentID != nil {
		parentID := strings.TrimSpace(*patch.ParentID)
		if parentID != "" {
			if _, ok := s.tasks[parentID]; !ok {
				return Snapshot{}, Task{}, fmt.Errorf("%w: parent task does not exist", errBadInput)
			}
			if s.wouldCycleLocked(id, parentID) {
				return Snapshot{}, Task{}, fmt.Errorf("%w: parent would create a cycle", errBadInput)
			}
		}
		task.ParentID = parentID
	}

	task.UpdatedAt = time.Now().UTC()
	s.tasks[id] = task
	s.updatedAt = task.UpdatedAt
	if err := s.persistLocked(); err != nil {
		return Snapshot{}, Task{}, err
	}

	return s.snapshotLocked(), task, nil
}

func (s *Store) DeleteTask(id string) (Snapshot, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Snapshot{}, fmt.Errorf("%w: id is required", errBadInput)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[id]; !ok {
		return Snapshot{}, errNotFound
	}

	for childID, child := range s.tasks {
		if child.ParentID == id {
			child.ParentID = ""
			child.UpdatedAt = time.Now().UTC()
			s.tasks[childID] = child
		}
	}
	delete(s.tasks, id)

	s.updatedAt = time.Now().UTC()
	if err := s.persistLocked(); err != nil {
		return Snapshot{}, err
	}

	return s.snapshotLocked(), nil
}

func (s *Store) load() error {
	bytes, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	var state persistedState
	if err := json.Unmarshal(bytes, &state); err != nil {
		return err
	}

	for _, task := range state.Tasks {
		if task.ID == "" {
			continue
		}
		s.tasks[task.ID] = task
	}
	if !state.UpdatedAt.IsZero() {
		s.updatedAt = state.UpdatedAt
	}
	return nil
}

func (s *Store) seedDefaults() error {
	now := time.Now().UTC()
	rootID, err := newID("task")
	if err != nil {
		return err
	}
	childID, err := newID("task")
	if err != nil {
		return err
	}
	networkID, err := newID("task")
	if err != nil {
		return err
	}

	s.tasks[rootID] = Task{
		ID:        rootID,
		Title:     "Do-It",
		Notes:     "A local-first task graph running from one Go server.",
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.tasks[childID] = Task{
		ID:        childID,
		Title:     "Build the mind map view",
		Notes:     "Tasks become connected nodes instead of a flat list.",
		ParentID:  rootID,
		CreatedAt: now.Add(time.Millisecond),
		UpdatedAt: now.Add(time.Millisecond),
	}
	s.tasks[networkID] = Task{
		ID:        networkID,
		Title:     "Test from another device",
		Notes:     "Open the LAN URL while connected to the same Wi-Fi.",
		ParentID:  rootID,
		CreatedAt: now.Add(2 * time.Millisecond),
		UpdatedAt: now.Add(2 * time.Millisecond),
	}
	s.updatedAt = now
	return nil
}

func (s *Store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	state := persistedState{
		Tasks:     s.sortedTasksLocked(),
		UpdatedAt: s.updatedAt,
	}
	bytes, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, bytes, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

func (s *Store) snapshotLocked() Snapshot {
	return Snapshot{
		Tasks:     s.sortedTasksLocked(),
		UpdatedAt: s.updatedAt,
	}
}

func (s *Store) sortedTasksLocked() []Task {
	tasks := make([]Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if !tasks[i].CreatedAt.Equal(tasks[j].CreatedAt) {
			return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
		}
		if tasks[i].Title != tasks[j].Title {
			return tasks[i].Title < tasks[j].Title
		}
		return tasks[i].ID < tasks[j].ID
	})
	return tasks
}

func (s *Store) wouldCycleLocked(id, parentID string) bool {
	for parentID != "" {
		if parentID == id {
			return true
		}
		parent, ok := s.tasks[parentID]
		if !ok {
			return false
		}
		parentID = parent.ParentID
	}
	return false
}

func newID(prefix string) (string, error) {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(bytes[:]), nil
}
