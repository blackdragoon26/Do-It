package main

import (
	"path/filepath"
	"testing"
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
