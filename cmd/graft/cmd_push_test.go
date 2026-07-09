package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestResolvePushRefNames(t *testing.T) {
	r, err := repo.Init(t.TempDir())
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	tests := []struct {
		name       string
		branchArg  string
		wantLabel  string
		wantLocal  string
		wantRemote string
		wantErr    bool
	}{
		{
			name:       "short branch name",
			branchArg:  "main",
			wantLabel:  "branch main",
			wantLocal:  "refs/heads/main",
			wantRemote: "heads/main",
		},
		{
			name:       "full branch ref",
			branchArg:  "refs/heads/feature",
			wantLabel:  "branch feature",
			wantLocal:  "refs/heads/feature",
			wantRemote: "heads/feature",
		},
		{
			name:       "full tag ref",
			branchArg:  "refs/tags/v1.0.0",
			wantLabel:  "tag v1.0.0",
			wantLocal:  "refs/tags/v1.0.0",
			wantRemote: "tags/v1.0.0",
		},
		{
			name:      "unsupported ref namespace",
			branchArg: "refs/notes/release",
			wantErr:   true,
		},
		{
			name:       "infer from HEAD when empty",
			branchArg:  "",
			wantLabel:  "branch main",
			wantLocal:  "refs/heads/main",
			wantRemote: "heads/main",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			label, localRef, remoteRef, err := resolvePushRefNames(r, tc.branchArg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolvePushRefNames: %v", err)
			}
			if label != tc.wantLabel {
				t.Fatalf("label = %q, want %q", label, tc.wantLabel)
			}
			if localRef != tc.wantLocal {
				t.Fatalf("localRef = %q, want %q", localRef, tc.wantLocal)
			}
			if remoteRef != tc.wantRemote {
				t.Fatalf("remoteRef = %q, want %q", remoteRef, tc.wantRemote)
			}
		})
	}
}

func TestShouldUseResumablePackPushRequiresAdvertisedCapability(t *testing.T) {
	clientWithoutCaps, err := remote.NewClient("https://example.com/graft/alice/repo")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if shouldUseResumablePackPush(clientWithoutCaps) {
		t.Fatal("resumable pack should be disabled before server capabilities are known")
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Graft-Capabilities", "pack,zstd,resumable-pack")
		_, _ = w.Write([]byte(`{"refs":{}}`))
	}))
	defer ts.Close()

	client, err := remote.NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.ListRefs(t.Context()); err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	if !shouldUseResumablePackPush(client) {
		t.Fatal("resumable pack should be enabled after server advertises pack,zstd,resumable-pack")
	}
}

func TestPushObjectsChunkedPrefersPackTransport(t *testing.T) {
	var packRequests, ndjsonRequests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graft/alice/repo/objects" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		switch r.Header.Get("Content-Type") {
		case "application/x-graft-pack":
			packRequests++
			if r.Header.Get("Content-Encoding") != "zstd" {
				t.Fatalf("Content-Encoding = %q, want zstd", r.Header.Get("Content-Encoding"))
			}
		case "application/x-ndjson":
			ndjsonRequests++
		default:
			t.Fatalf("unexpected Content-Type %q", r.Header.Get("Content-Type"))
		}
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"received":1}`))
	}))
	defer ts.Close()

	client, err := remote.NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	blobData := object.MarshalBlob(&object.Blob{Data: []byte("hello\n")})
	uploaded, err := pushObjectsChunked(context.Background(), client, []remote.ObjectRecord{
		{Hash: object.HashObject(object.TypeBlob, blobData), Type: object.TypeBlob, Data: blobData},
	})
	if err != nil {
		t.Fatalf("pushObjectsChunked: %v", err)
	}
	if uploaded != 1 {
		t.Fatalf("uploaded = %d, want 1", uploaded)
	}
	if packRequests != 1 {
		t.Fatalf("packRequests = %d, want 1", packRequests)
	}
	if ndjsonRequests != 0 {
		t.Fatalf("ndjsonRequests = %d, want 0", ndjsonRequests)
	}
}

func TestPushObjectsChunkedFallsBackWhenPackUnsupported(t *testing.T) {
	var packRequests, ndjsonRequests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graft/alice/repo/objects" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		switch r.Header.Get("Content-Type") {
		case "application/x-graft-pack":
			packRequests++
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		case "application/x-ndjson":
			ndjsonRequests++
			_, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"received":1}`))
			return
		default:
			t.Fatalf("unexpected Content-Type %q", r.Header.Get("Content-Type"))
		}
	}))
	defer ts.Close()

	client, err := remote.NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	blobData := object.MarshalBlob(&object.Blob{Data: []byte("hello\n")})
	uploaded, err := pushObjectsChunked(context.Background(), client, []remote.ObjectRecord{
		{Hash: object.HashObject(object.TypeBlob, blobData), Type: object.TypeBlob, Data: blobData},
	})
	if err != nil {
		t.Fatalf("pushObjectsChunked: %v", err)
	}
	if uploaded != 1 {
		t.Fatalf("uploaded = %d, want 1", uploaded)
	}
	if packRequests != 1 {
		t.Fatalf("packRequests = %d, want 1", packRequests)
	}
	if ndjsonRequests != 1 {
		t.Fatalf("ndjsonRequests = %d, want 1", ndjsonRequests)
	}
}

func TestPushObjectsChunkedHonorsAdvertisedMaxBatch(t *testing.T) {
	var objectRequests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/graft/alice/repo/refs":
			w.Header().Set("Graft-Capabilities", "pack,zstd")
			w.Header().Set("Graft-Limits", "max_batch=1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"refs":{}}`))
			return
		case r.Method == http.MethodPost && r.URL.Path == "/graft/alice/repo/objects":
			objectRequests++
			_, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"received":1}`))
			return
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client, err := remote.NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.ListRefs(context.Background()); err != nil {
		t.Fatalf("ListRefs: %v", err)
	}

	blobA := object.MarshalBlob(&object.Blob{Data: []byte("a\n")})
	blobB := object.MarshalBlob(&object.Blob{Data: []byte("b\n")})
	uploaded, err := pushObjectsChunked(context.Background(), client, []remote.ObjectRecord{
		{Hash: object.HashObject(object.TypeBlob, blobA), Type: object.TypeBlob, Data: blobA},
		{Hash: object.HashObject(object.TypeBlob, blobB), Type: object.TypeBlob, Data: blobB},
	})
	if err != nil {
		t.Fatalf("pushObjectsChunked: %v", err)
	}
	if uploaded != 2 {
		t.Fatalf("uploaded = %d, want 2", uploaded)
	}
	if objectRequests != 2 {
		t.Fatalf("objectRequests = %d, want 2", objectRequests)
	}
}

func TestPushCmdCheckRejectsOversizedObject(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	large := bytes.Repeat([]byte("a"), pushObjectByteLimit+1)
	if err := os.WriteFile(filepath.Join(dir, "large.bin"), large, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"large.bin"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("large", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/refs") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"refs":{}}`))
	}))
	defer ts.Close()

	if err := r.SetRemote("origin", ts.URL+"/graft/alice/repo"); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newPushCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--check"})

	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected push --check to fail for an oversized object")
	}
	if !strings.Contains(err.Error(), "push limit check failed") {
		t.Fatalf("error = %q, want push limit failure", err.Error())
	}
	if !strings.Contains(err.Error(), "16.0 MiB") {
		t.Fatalf("error = %q, want formatted object limit", err.Error())
	}
}

func TestPushCmdCheckUsesRemoteMaxObjectLimit(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "small.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"small.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("small", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/refs") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Graft-Limits", "max_object=4")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"refs":{}}`))
	}))
	defer ts.Close()

	if err := r.SetRemote("origin", ts.URL+"/graft/alice/repo"); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newPushCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--check"})

	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected push --check to fail for the remote object limit")
	}
	if !strings.Contains(err.Error(), "push limit check failed") {
		t.Fatalf("error = %q, want push limit failure", err.Error())
	}
	if !strings.Contains(err.Error(), "4 B") {
		t.Fatalf("error = %q, want remote object limit", err.Error())
	}
}

func TestPushCmdRequireSignedRejectsUnsignedBranchBeforeUpload(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("unsigned", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	remoteCalled := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		remoteCalled = true
		http.Error(w, "signature policy should run before remote calls", http.StatusInternalServerError)
	}))
	defer ts.Close()

	if err := r.SetRemote("origin", ts.URL+"/graft/alice/repo"); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newPushCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--check", "--require-signed"})

	err = cmd.Execute()
	if err == nil {
		t.Fatal("push --check succeeded, want signature policy failure")
	}
	if got := commandExitCode(err); got != exitVerificationFailure {
		t.Fatalf("exit code = %d, want %d; err=%v", got, exitVerificationFailure, err)
	}
	if !strings.Contains(err.Error(), "unsigned commit") {
		t.Fatalf("error = %q, want unsigned commit failure", err.Error())
	}
	if remoteCalled {
		t.Fatal("remote was called before local signature policy failed")
	}
}

func TestPushCmdRequireSignedAllowsTrustedSignedTagCheck(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	head, err := r.Commit("initial", "tester")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	keyPath := filepath.Join(dir, "id_ed25519")
	if err := repo.GenerateSigningKey(keyPath); err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}
	signer, err := repo.NewSSHSigner(keyPath)
	if err != nil {
		t.Fatalf("NewSSHSigner: %v", err)
	}
	if _, err := r.CreateAnnotatedTagWithSigner("v1.0.0", head, "tester", "release", false, signer); err != nil {
		t.Fatalf("CreateAnnotatedTagWithSigner: %v", err)
	}
	allowedSignersPath := filepath.Join(dir, "allowed_signers")
	writeAllowedSignerForKey(t, allowedSignersPath, "release@example.com", keyPath)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/refs") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"refs":{}}`))
	}))
	defer ts.Close()

	if err := r.SetRemote("origin", ts.URL+"/graft/alice/repo"); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newPushCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--check", "--require-signed", "--allowed-signers", allowedSignersPath, "refs/tags/v1.0.0"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("push --check Execute: %v", err)
	}
}

func TestPushDoesNotUpdateRefsWhenObjectUploadFails(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	var objectUploads int
	var refUpdates int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/graft/alice/repo/refs":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"refs":{}}`))
		case req.Method == http.MethodPost && req.URL.Path == "/graft/alice/repo/objects":
			objectUploads++
			http.Error(w, "object upload failed", http.StatusInternalServerError)
		case req.Method == http.MethodPost && req.URL.Path == "/graft/alice/repo/refs":
			refUpdates++
			http.Error(w, "refs must not be updated after object upload failure", http.StatusInternalServerError)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	if err := r.SetRemote("origin", ts.URL+"/graft/alice/repo"); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newPushCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err = cmd.Execute()
	if err == nil {
		t.Fatal("push succeeded, want object upload failure")
	}
	if objectUploads == 0 {
		t.Fatal("object upload endpoint was not called")
	}
	if refUpdates != 0 {
		t.Fatalf("ref update endpoint called %d time(s), want 0", refUpdates)
	}
	if _, err := r.ResolveRef(remoteTrackingRefName("origin", "heads/main")); err == nil {
		t.Fatal("remote tracking ref was updated despite failed push")
	}
}
