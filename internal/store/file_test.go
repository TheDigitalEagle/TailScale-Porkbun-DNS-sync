package store

import (
	"context"
	"path/filepath"
	"testing"

	"porkbun-dns/internal/model"
)

func TestFileStoreLifecycle(t *testing.T) {
	t.Parallel()

	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	ctx := context.Background()

	if err := store.Put(ctx, model.Entry{Name: "pihole.int.ima.fish"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	entry, ok, err := store.Get(ctx, "pihole.int.ima.fish")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if got, want := entry.Name, "pihole.int.ima.fish"; got != want {
		t.Fatalf("entry.Name = %q, want %q", got, want)
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if got, want := len(entries), 1; got != want {
		t.Fatalf("len(entries) = %d, want %d", got, want)
	}

	_, ok, err = store.Delete(ctx, "pihole.int.ima.fish")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !ok {
		t.Fatal("Delete() ok = false, want true")
	}
}
