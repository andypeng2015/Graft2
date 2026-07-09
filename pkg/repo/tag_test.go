package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestTagCreateResolveAndList(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	head, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := r.CreateTag("v1.0.0", head, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	resolved, err := r.ResolveTag("v1.0.0")
	if err != nil {
		t.Fatalf("ResolveTag: %v", err)
	}
	if resolved != head {
		t.Fatalf("resolved tag = %q, want %q", resolved, head)
	}

	tags, err := r.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 1 || tags[0] != "v1.0.0" {
		t.Fatalf("ListTags = %v, want [v1.0.0]", tags)
	}
}

func TestTagCreateExistingWithoutForceFails(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	head, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := r.CreateTag("v1.0.0", head, false); err != nil {
		t.Fatalf("CreateTag first: %v", err)
	}
	if err := r.CreateTag("v1.0.0", head, false); err == nil {
		t.Fatalf("CreateTag second without force should fail")
	}
}

func TestTagCreateForceUpdatesTarget(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	h1, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit h1: %v", err)
	}

	if err := r.CreateTag("v1.0.0", h1, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h2, err := r.Commit("second", "test-author")
	if err != nil {
		t.Fatalf("Commit h2: %v", err)
	}

	if err := r.CreateTag("v1.0.0", h2, true); err != nil {
		t.Fatalf("CreateTag force: %v", err)
	}
	resolved, err := r.ResolveTag("v1.0.0")
	if err != nil {
		t.Fatalf("ResolveTag: %v", err)
	}
	if resolved != h2 {
		t.Fatalf("resolved tag = %q, want %q", resolved, h2)
	}
}

func TestTagDelete(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	head, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := r.CreateTag("v1.0.0", head, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	if err := r.DeleteTag("v1.0.0"); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	if _, err := r.ResolveTag("v1.0.0"); err == nil {
		t.Fatalf("ResolveTag should fail after delete")
	}
}

func TestCreateAnnotatedTagStoresTagObjectAndRef(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	head, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	tagHash, err := r.CreateAnnotatedTag("v1.0.0", head, "Alice <alice@example.com>", "release 1.0.0", false)
	if err != nil {
		t.Fatalf("CreateAnnotatedTag: %v", err)
	}
	if tagHash == "" {
		t.Fatalf("CreateAnnotatedTag returned empty hash")
	}
	if tagHash == head {
		t.Fatalf("annotated tag hash should differ from target commit hash")
	}

	resolvedRef, err := r.ResolveTag("v1.0.0")
	if err != nil {
		t.Fatalf("ResolveTag: %v", err)
	}
	if resolvedRef != tagHash {
		t.Fatalf("resolved tag ref = %q, want %q", resolvedRef, tagHash)
	}

	tag, err := r.Store.ReadTag(tagHash)
	if err != nil {
		t.Fatalf("ReadTag(%s): %v", tagHash, err)
	}
	if tag.TargetHash != head {
		t.Fatalf("tag target = %q, want %q", tag.TargetHash, head)
	}
	data := string(tag.Data)
	if !strings.Contains(data, "object "+string(head)+"\n") {
		t.Fatalf("tag payload missing object header: %q", data)
	}
	if !strings.Contains(data, "type commit\n") {
		t.Fatalf("tag payload missing commit type: %q", data)
	}
	if !strings.Contains(data, "tag v1.0.0\n") {
		t.Fatalf("tag payload missing name: %q", data)
	}
	if !strings.Contains(data, "tagger Alice <alice@example.com> ") {
		t.Fatalf("tag payload missing tagger: %q", data)
	}
	if !strings.Contains(data, "\n\nrelease 1.0.0\n") {
		t.Fatalf("tag payload missing message: %q", data)
	}
}

func TestCreateAnnotatedTagWithSignerStoresVerifiableSignature(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	head, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := GenerateSigningKey(keyPath); err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}
	signer, err := NewSSHSigner(keyPath)
	if err != nil {
		t.Fatalf("NewSSHSigner: %v", err)
	}

	tagHash, err := r.CreateAnnotatedTagWithSigner("v1.0.0", head, "Alice <alice@example.com>", "release 1.0.0", false, signer)
	if err != nil {
		t.Fatalf("CreateAnnotatedTagWithSigner: %v", err)
	}
	tag, err := r.Store.ReadTag(tagHash)
	if err != nil {
		t.Fatalf("ReadTag(%s): %v", tagHash, err)
	}
	if object.TagSignature(tag.Data) == "" {
		t.Fatalf("tag signature is empty:\n%s", tag.Data)
	}
	if strings.Contains(string(object.TagSigningPayload(tag.Data)), "\nsignature ") {
		t.Fatalf("tag signing payload contains signature header:\n%s", object.TagSigningPayload(tag.Data))
	}

	result, err := r.VerifyTagSignature("v1.0.0")
	if err != nil {
		t.Fatalf("VerifyTagSignature: %v", err)
	}
	if !result.Valid || result.Unsigned || result.Algorithm != "ssh-ed25519" || result.TagHash != tagHash || result.TargetHash != head {
		t.Fatalf("unexpected verification result: %+v", result)
	}
}

func TestVerifyTagAgainstAllowedSigners(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	head, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := GenerateSigningKey(keyPath); err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}
	signer, err := NewSSHSigner(keyPath)
	if err != nil {
		t.Fatalf("NewSSHSigner: %v", err)
	}
	if _, err := r.CreateAnnotatedTagWithSigner("v1.0.0", head, "Alice <alice@example.com>", "release 1.0.0", false, signer); err != nil {
		t.Fatalf("CreateAnnotatedTagWithSigner: %v", err)
	}

	pubKey, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("ReadFile public key: %v", err)
	}
	allowedPath := filepath.Join(dir, "allowed_signers")
	if err := os.WriteFile(allowedPath, []byte("release@example.com "+strings.TrimSpace(string(pubKey))+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile allowed_signers: %v", err)
	}
	signers, err := LoadAllowedSigners(allowedPath)
	if err != nil {
		t.Fatalf("LoadAllowedSigners: %v", err)
	}
	result, err := r.VerifyTagAgainstAllowedSigners("v1.0.0", signers)
	if err != nil {
		t.Fatalf("VerifyTagAgainstAllowedSigners: %v", err)
	}
	if !result.Valid || result.SignerKey != "release@example.com" {
		t.Fatalf("unexpected verification result: %+v", result)
	}
}

func TestVerifyTagSignatureLightweightUnsigned(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	head, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := r.CreateTag("v1.0.0", head, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	result, err := r.VerifyTagSignature("v1.0.0")
	if err != nil {
		t.Fatalf("VerifyTagSignature: %v", err)
	}
	if !result.Unsigned || result.Valid || result.TargetHash != head {
		t.Fatalf("unexpected verification result: %+v", result)
	}
}

func TestCreateAnnotatedTagRequiresMessage(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	head, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, err := r.CreateAnnotatedTag("v1.0.0", head, "Alice <alice@example.com>", "   ", false); err == nil {
		t.Fatalf("expected CreateAnnotatedTag to fail without message")
	}
}
