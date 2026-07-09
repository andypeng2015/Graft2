package main

import (
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestMarkClonedHooksUntrusted(t *testing.T) {
	r, err := repo.Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	trusted, err := r.HooksTrusted()
	if err != nil {
		t.Fatalf("HooksTrusted: %v", err)
	}
	if !trusted {
		t.Fatal("fresh repo hooks trusted = false, want true before clone mark")
	}

	if err := markClonedHooksUntrusted(r); err != nil {
		t.Fatalf("markClonedHooksUntrusted: %v", err)
	}
	trusted, err = r.HooksTrusted()
	if err != nil {
		t.Fatalf("HooksTrusted after mark: %v", err)
	}
	if trusted {
		t.Fatal("cloned repo hooks trusted = true, want false")
	}
}
