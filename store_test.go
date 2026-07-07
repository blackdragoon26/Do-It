package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreTaskLifecyclePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	snapshot, parent, err := store.AddTask("Parent", "root notes", "", nil)
	if err != nil {
		t.Fatalf("add parent: %v", err)
	}
	if len(snapshot.Tasks) == 0 {
		t.Fatal("expected tasks in snapshot")
	}

	_, child, err := store.AddTask("Child", "", parent.ID, nil)
	if err != nil {
		t.Fatalf("add child: %v", err)
	}
	if child.ParentID != parent.ID {
		t.Fatalf("expected parent %q, got %q", parent.ID, child.ParentID)
	}

	done := true
	_, patched, err := store.PatchTask(child.ID, TaskPatch{Done: &done})
	if err != nil {
		t.Fatalf("patch child: %v", err)
	}
	if !patched.Done {
		t.Fatal("expected patched task to be done")
	}

	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	found := false
	for _, task := range reloaded.Snapshot().Tasks {
		if task.ID == child.ID {
			found = true
			if !task.Done {
				t.Fatal("expected done state to persist")
			}
		}
	}
	if !found {
		t.Fatal("expected child task after reload")
	}
}

func TestStoreRejectsParentCycles(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	_, parent, err := store.AddTask("Parent", "", "", nil)
	if err != nil {
		t.Fatalf("add parent: %v", err)
	}
	_, child, err := store.AddTask("Child", "", parent.ID, nil)
	if err != nil {
		t.Fatalf("add child: %v", err)
	}

	newParent := child.ID
	if _, _, err := store.PatchTask(parent.ID, TaskPatch{ParentID: &newParent}); err == nil {
		t.Fatal("expected cycle to be rejected")
	}
}

func TestStoreTaskReturnsAttachmentCopy(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	_, task, err := store.AddTask("With attachment", "", "", []Attachment{{
		ID:   "file_1",
		Name: "notes.txt",
		URL:  "/uploads/file_1_notes.txt",
	}})
	if err != nil {
		t.Fatalf("add task: %v", err)
	}

	got, err := store.Task(task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	got.Attachments[0].Name = "mutated.txt"

	again, err := store.Task(task.ID)
	if err != nil {
		t.Fatalf("get task again: %v", err)
	}
	if again.Attachments[0].Name != "notes.txt" {
		t.Fatalf("expected stored attachment to be unchanged, got %q", again.Attachments[0].Name)
	}
}

func TestStoreSortsTasksWithIDTieBreaker(t *testing.T) {
	store := &Store{
		tasks: make(map[string]Task),
	}
	createdAt := time.Date(2026, 6, 22, 13, 8, 34, 0, time.UTC)
	store.tasks["task_b"] = Task{ID: "task_b", Title: "Same", CreatedAt: createdAt}
	store.tasks["task_a"] = Task{ID: "task_a", Title: "Same", CreatedAt: createdAt}

	tasks := store.sortedTasksLocked()
	if len(tasks) != 2 {
		t.Fatalf("expected two tasks, got %d", len(tasks))
	}
	if tasks[0].ID != "task_a" || tasks[1].ID != "task_b" {
		t.Fatalf("expected ID tie-breaker order, got %q then %q", tasks[0].ID, tasks[1].ID)
	}
}

func TestStoreEnforcesTaskTextLimits(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	longTitle := strings.Repeat("t", maxTitleLength+1)
	if _, _, err := store.AddTask(longTitle, "", "", nil); err == nil {
		t.Fatal("expected long title to be rejected")
	}

	_, task, err := store.AddTask("Within limit", "", "", nil)
	if err != nil {
		t.Fatalf("add task: %v", err)
	}
	longNotes := strings.Repeat("n", maxNotesLength+1)
	if _, _, err := store.PatchTask(task.ID, TaskPatch{Notes: &longNotes}); err == nil {
		t.Fatal("expected long notes to be rejected")
	}
}

func TestStoreTaskTextLimitsMatchUTF16MaxLength(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	withinLimit := strings.Repeat("a", maxTitleLength-2) + "😀"
	if _, _, err := store.AddTask(withinLimit, "", "", nil); err != nil {
		t.Fatalf("expected UTF-16 limit boundary to be accepted: %v", err)
	}

	overLimit := strings.Repeat("a", maxTitleLength-1) + "😀"
	if _, _, err := store.AddTask(overLimit, "", "", nil); err == nil {
		t.Fatal("expected title beyond UTF-16 limit to be rejected")
	}
}
