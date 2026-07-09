package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/userconfig"
)

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantBase   string
		wantOwner  string
		wantRepo   string
		shouldFail bool
	}{
		{
			name:      "canonical got path",
			in:        "https://example.com/graft/alice/proj",
			wantBase:  "https://example.com/graft/alice/proj",
			wantOwner: "alice",
			wantRepo:  "proj",
		},
		{
			name:      "plain owner repo path",
			in:        "https://example.com/alice/proj",
			wantBase:  "https://example.com/graft/alice/proj",
			wantOwner: "alice",
			wantRepo:  "proj",
		},
		{
			name:      "api prefix with got path",
			in:        "https://example.com/api/v1/graft/alice/proj",
			wantBase:  "https://example.com/api/v1/graft/alice/proj",
			wantOwner: "alice",
			wantRepo:  "proj",
		},
		{
			name:       "invalid",
			in:         "alice/proj",
			shouldFail: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ep, err := ParseEndpoint(tc.in)
			if tc.shouldFail {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseEndpoint: %v", err)
			}
			if ep.BaseURL != tc.wantBase {
				t.Fatalf("BaseURL = %q, want %q", ep.BaseURL, tc.wantBase)
			}
			if ep.Owner != tc.wantOwner {
				t.Fatalf("Owner = %q, want %q", ep.Owner, tc.wantOwner)
			}
			if ep.Repo != tc.wantRepo {
				t.Fatalf("Repo = %q, want %q", ep.Repo, tc.wantRepo)
			}
		})
	}
}

func TestEndpointOrchardBaseURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "root mounted orchard",
			in:   "https://example.com/graft/alice/proj",
			want: "https://example.com",
		},
		{
			name: "api prefix orchard",
			in:   "https://example.com/api/v1/graft/alice/proj",
			want: "https://example.com/api/v1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ep, err := ParseEndpoint(tc.in)
			if err != nil {
				t.Fatalf("ParseEndpoint: %v", err)
			}
			if got := ep.OrchardBaseURL(); got != tc.want {
				t.Fatalf("OrchardBaseURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewClientUsesHostScopedConfigToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := userconfig.Save(&userconfig.Config{
		OrchardURL: "https://orchard.example.com",
		Token:      "default-token",
		Username:   "default-user",
		Owner:      "default-owner",
		OrchardProfiles: map[string]userconfig.OrchardProfile{
			"https://code.example.com/api/v1": {
				Token:    "code-token",
				Username: "code-user",
				Owner:    "code-owner",
			},
		},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	client, err := NewClient("https://code.example.com/api/v1/graft/alice/repo")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.token != "code-token" {
		t.Fatalf("client.token = %q, want code-token", client.token)
	}
}

func TestNewClientEnvTokenOverridesConfigAndIsNotPersisted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFT_TOKEN", "env-token")

	if err := userconfig.Save(&userconfig.Config{
		OrchardURL: "https://orchard.example.com",
		Token:      "stored-token",
		OrchardProfiles: map[string]userconfig.OrchardProfile{
			"https://code.example.com/api/v1": {
				Token:    "code-token",
				Username: "code-user",
				Owner:    "code-owner",
			},
		},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	client, err := NewClient("https://code.example.com/api/v1/graft/alice/repo")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.token != "env-token" {
		t.Fatalf("client.token = %q, want env-token", client.token)
	}

	cfg, err := userconfig.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.OrchardProfile("https://code.example.com/api/v1").Token; got != "code-token" {
		t.Fatalf("stored profile token = %q, want code-token", got)
	}
	if cfg.Token != "stored-token" {
		t.Fatalf("top-level token = %q, want stored-token", cfg.Token)
	}
}

func TestNewClientDoesNotReuseOtherHostToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := userconfig.Save(&userconfig.Config{
		OrchardURL: "https://orchard.example.com",
		Token:      "default-token",
		Username:   "default-user",
		Owner:      "default-owner",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	client, err := NewClient("https://code.example.com/api/v1/graft/alice/repo")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.token != "" {
		t.Fatalf("client.token = %q, want empty for unmatched host", client.token)
	}
}

func TestPushObjectsIncludesComputedHash(t *testing.T) {
	var received int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graft/alice/repo/objects" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		type pushedObject struct {
			Hash string `json:"hash"`
			Type string `json:"type"`
			Data []byte `json:"data"`
		}
		dec := json.NewDecoder(r.Body)
		for {
			var obj pushedObject
			if err := dec.Decode(&obj); err != nil {
				break
			}
			received++
			objType := object.ObjectType(obj.Type)
			computed := object.HashObject(objType, obj.Data)
			if obj.Hash != string(computed) {
				t.Fatalf("expected pushed hash %s, got %s", computed, obj.Hash)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"received":1}`))
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}

	blobData := object.MarshalBlob(&object.Blob{Data: []byte("hello\n")})
	err = client.PushObjects(t.Context(), []ObjectRecord{
		{Type: object.TypeBlob, Data: blobData},
	})
	if err != nil {
		t.Fatalf("PushObjects: %v", err)
	}
	if received != 1 {
		t.Fatalf("expected 1 pushed object, got %d", received)
	}
}

func TestPushObjectsRejectsProvidedHashMismatch(t *testing.T) {
	var requests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}

	blobData := object.MarshalBlob(&object.Blob{Data: []byte("hello\n")})
	err = client.PushObjects(t.Context(), []ObjectRecord{
		{
			Hash: object.Hash(strings.Repeat("a", 64)),
			Type: object.TypeBlob,
			Data: blobData,
		},
	})
	if err == nil {
		t.Fatal("expected hash mismatch error")
	}
	if !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected hash mismatch error, got %v", err)
	}
	if requests != 0 {
		t.Fatalf("expected no HTTP requests on local hash mismatch, got %d", requests)
	}
}

func TestParseObjectTypeAcceptsTag(t *testing.T) {
	got, err := parseObjectType("tag")
	if err != nil {
		t.Fatalf("parseObjectType(tag): %v", err)
	}
	if got != object.TypeTag {
		t.Fatalf("parseObjectType(tag) = %q, want %q", got, object.TypeTag)
	}
}

func TestNewClientWithOptionsTimeout(t *testing.T) {
	client, err := NewClientWithOptions("https://example.com/graft/alice/repo", ClientOptions{
		Timeout:     120 * time.Second,
		MaxAttempts: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.httpClient.Timeout != 120*time.Second {
		t.Fatalf("timeout = %v, want 120s", client.httpClient.Timeout)
	}
	if client.maxAttempts != 5 {
		t.Fatalf("maxAttempts = %d, want 5", client.maxAttempts)
	}
}

func TestNewClientWithOptionsDefaults(t *testing.T) {
	client, err := NewClientWithOptions("https://example.com/graft/alice/repo", ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if client.httpClient.Timeout != 60*time.Second {
		t.Fatalf("timeout = %v, want 60s", client.httpClient.Timeout)
	}
	if client.maxAttempts != 3 {
		t.Fatalf("maxAttempts = %d, want 3", client.maxAttempts)
	}
}

func TestDoRejectsWrongContentType(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>error</html>"))
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListRefs(t.Context())
	if err == nil {
		t.Fatal("expected content-type error")
	}
	if !strings.Contains(err.Error(), "text/html") {
		t.Fatalf("expected content-type in error, got: %v", err)
	}
}

func TestListRefsRejectsInvalidHash(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"heads/main": "not-a-hash",
		})
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListRefs(t.Context())
	if err == nil {
		t.Fatal("expected hash validation error")
	}
}

func TestClientSendsCapabilityHeaders(t *testing.T) {
	var graftProtocol, graftCaps string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		graftProtocol = r.Header.Get("Graft-Protocol")
		graftCaps = r.Header.Get("Graft-Capabilities")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.ListRefs(t.Context())

	if graftProtocol != ProtocolVersion {
		t.Fatalf("Graft-Protocol = %q, want %q", graftProtocol, ProtocolVersion)
	}
	if graftCaps == "" {
		t.Fatal("Graft-Capabilities header missing")
	}
}

func TestListRefsHandlesLargeResponse(t *testing.T) {
	refs := make(map[string]string)
	for i := 0; i < 1000; i++ {
		refs[fmt.Sprintf("heads/branch-%04d", i)] = strings.Repeat("a", 64)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(refs)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.ListRefs(t.Context())
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	if len(got) != 1000 {
		t.Fatalf("graft %d refs, want 1000", len(got))
	}
}

func TestListRefsPaginated(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		cursor := r.URL.Query().Get("cursor")

		switch cursor {
		case "":
			// First page
			_ = json.NewEncoder(w).Encode(map[string]any{
				"refs":   map[string]string{"heads/main": strings.Repeat("a", 64)},
				"cursor": "page2",
			})
		case "page2":
			// Second page
			_ = json.NewEncoder(w).Encode(map[string]any{
				"refs":   map[string]string{"heads/dev": strings.Repeat("b", 64)},
				"cursor": "page3",
			})
		case "page3":
			// Last page (no cursor)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"refs": map[string]string{"tags/v1": strings.Repeat("c", 64)},
			})
		default:
			http.Error(w, "unexpected cursor", http.StatusBadRequest)
		}
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	refs, err := client.ListRefs(t.Context())
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("graft %d refs, want 3", len(refs))
	}
	if _, ok := refs["heads/main"]; !ok {
		t.Fatal("missing heads/main")
	}
	if _, ok := refs["heads/dev"]; !ok {
		t.Fatal("missing heads/dev")
	}
	if _, ok := refs["tags/v1"]; !ok {
		t.Fatal("missing tags/v1")
	}
	if calls != 3 {
		t.Fatalf("expected 3 page requests, got %d", calls)
	}
}

func TestListRefsLegacyFormat(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Legacy flat-map format without "refs" wrapper
		_ = json.NewEncoder(w).Encode(map[string]string{
			"heads/main": strings.Repeat("a", 64),
		})
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	refs, err := client.ListRefs(t.Context())
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("graft %d refs, want 1", len(refs))
	}
	if _, ok := refs["heads/main"]; !ok {
		t.Fatal("missing heads/main")
	}
}

func TestProtocolConformanceSmokeAgainstMockOrchard(t *testing.T) {
	blobData := object.MarshalBlob(&object.Blob{Data: []byte("conformance\n")})
	blobHash := object.HashObject(object.TypeBlob, blobData)
	updatedHash := object.Hash(strings.Repeat("b", 64))
	seen := map[string]int{}
	seenContractEndpoints := map[string]int{}
	markContractEndpoint := func(method, path string) {
		seenContractEndpoints[method+" "+path]++
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(headerProtocol) != ProtocolVersion {
			t.Errorf("%s %s: %s = %q, want %q", r.Method, r.URL.Path, headerProtocol, r.Header.Get(headerProtocol), ProtocolVersion)
			http.Error(w, "bad protocol header", http.StatusBadRequest)
			return
		}
		if r.Header.Get(headerCapabilities) == "" {
			t.Errorf("%s %s: missing %s", r.Method, r.URL.Path, headerCapabilities)
			http.Error(w, "missing capability header", http.StatusBadRequest)
			return
		}

		w.Header().Set("Graft-Capabilities", "pack,zstd")
		w.Header().Set("Graft-Limits", "max_batch=128,max_payload=1048576,max_object=1048576")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/graft/alice/repo/refs":
			seen["GET refs"]++
			markContractEndpoint(http.MethodGet, "{base}/refs")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"refs": map[string]string{"heads/main": string(blobHash)},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/graft/alice/repo/objects/"+string(blobHash):
			seen["GET object"]++
			markContractEndpoint(http.MethodGet, "{base}/objects/{hash}")
			w.Header().Set("X-Object-Type", string(object.TypeBlob))
			_, _ = w.Write(blobData)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/graft/alice/repo/objects/batch" && strings.Contains(r.Header.Get("Accept"), "application/x-graft-pack"):
			seen["POST batch pack"]++
			markContractEndpoint(http.MethodPost, "{base}/objects/batch")
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("batch Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
			}
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "zstd") {
				t.Errorf("batch Accept-Encoding = %q, want zstd", r.Header.Get("Accept-Encoding"))
			}
			var req struct {
				Wants []string `json:"wants"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(req.Wants) != 1 || req.Wants[0] != string(blobHash) {
				t.Errorf("batch wants = %#v, want [%s]", req.Wants, blobHash)
			}
			pack, err := EncodePackTransportToBytes([]ObjectRecord{{
				Hash: blobHash,
				Type: object.TypeBlob,
				Data: blobData,
			}})
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			compressed, err := compressZstd(pack)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/x-graft-pack")
			w.Header().Set("Content-Encoding", "zstd")
			w.Header().Set("X-Truncated", "false")
			_, _ = w.Write(compressed)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/graft/alice/repo/objects/batch":
			seen["POST batch json"]++
			markContractEndpoint(http.MethodPost, "{base}/objects/batch")
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("batch Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
			}
			var req struct {
				Wants []string `json:"wants"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(req.Wants) != 1 || req.Wants[0] != string(blobHash) {
				t.Errorf("batch wants = %#v, want [%s]", req.Wants, blobHash)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"objects": []map[string]any{{
					"hash": string(blobHash),
					"type": string(object.TypeBlob),
					"data": blobData,
				}},
				"truncated": false,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/graft/alice/repo/objects" && r.Header.Get("Content-Type") == "application/x-graft-pack":
			seen["POST objects pack"]++
			markContractEndpoint(http.MethodPost, "{base}/objects")
			if r.Header.Get("Content-Encoding") != "zstd" {
				t.Errorf("objects Content-Encoding = %q, want zstd", r.Header.Get("Content-Encoding"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			pack, err := decompressZstd(body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			records, err := DecodePackTransport(pack)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(records) != 1 || records[0].Hash != blobHash || records[0].Type != object.TypeBlob || string(records[0].Data) != string(blobData) {
				t.Errorf("pushed pack records = %#v, want %s/%s", records, blobHash, object.TypeBlob)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"received":1}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/graft/alice/repo/objects/resumable":
			seen["POST objects resumable"]++
			markContractEndpoint(http.MethodPost, "{base}/objects/resumable")
			if r.Header.Get("Content-Type") != "application/x-graft-pack-chunk" {
				t.Errorf("resumable Content-Type = %q, want application/x-graft-pack-chunk", r.Header.Get("Content-Type"))
			}
			if r.Header.Get("Content-Encoding") != "zstd" {
				t.Errorf("resumable Content-Encoding = %q, want zstd", r.Header.Get("Content-Encoding"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if got, want := sha256Hex(body), r.Header.Get("Graft-Chunk-SHA256"); got != want {
				t.Errorf("chunk hash = %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"upload_id":   "conformance-upload",
				"retry_token": "conformance-token",
				"received":    len(body),
				"complete":    true,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/graft/alice/repo/objects":
			seen["POST objects ndjson"]++
			markContractEndpoint(http.MethodPost, "{base}/objects")
			if r.Header.Get("Content-Type") != "application/x-ndjson" {
				t.Errorf("objects Content-Type = %q, want application/x-ndjson", r.Header.Get("Content-Type"))
			}
			dec := json.NewDecoder(r.Body)
			var received int
			for {
				var obj struct {
					Hash string `json:"hash"`
					Type string `json:"type"`
					Data []byte `json:"data"`
				}
				if err := dec.Decode(&obj); err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				received++
				if obj.Hash != string(blobHash) || obj.Type != string(object.TypeBlob) || string(obj.Data) != string(blobData) {
					t.Errorf("pushed object = %#v, want %s/%s", obj, blobHash, object.TypeBlob)
				}
			}
			if received != 1 {
				t.Errorf("received %d pushed objects, want 1", received)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"received":1}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/graft/alice/repo/refs":
			seen["POST refs"]++
			markContractEndpoint(http.MethodPost, "{base}/refs")
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("refs Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
			}
			var req struct {
				Updates []struct {
					Name string `json:"name"`
					New  string `json:"new"`
				} `json:"updates"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(req.Updates) != 1 || req.Updates[0].Name != "heads/main" || req.Updates[0].New != string(updatedHash) {
				t.Errorf("ref updates = %#v, want heads/main -> %s", req.Updates, updatedHash)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"updated": map[string]string{"heads/main": string(updatedHash)},
			})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	contract := SupportedProtocolContract()
	for _, endpoint := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "{base}/refs"},
		{http.MethodGet, "{base}/objects/{hash}"},
		{http.MethodPost, "{base}/objects/batch"},
		{http.MethodPost, "{base}/objects"},
		{http.MethodPost, "{base}/objects/resumable"},
		{http.MethodPost, "{base}/refs"},
	} {
		if !protocolEndpointExists(contract.Endpoints, endpoint.method, endpoint.path) {
			t.Fatalf("contract missing %s %s", endpoint.method, endpoint.path)
		}
	}

	client, err := NewClient(ts.URL + "/api/v1/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}

	refs, err := client.ListRefs(t.Context())
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	if refs["heads/main"] != blobHash {
		t.Fatalf("heads/main = %s, want %s", refs["heads/main"], blobHash)
	}

	obj, err := client.GetObject(t.Context(), blobHash)
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if obj.Hash != blobHash || obj.Type != object.TypeBlob || string(obj.Data) != string(blobData) {
		t.Fatalf("object = %#v, want %s/%s", obj, blobHash, object.TypeBlob)
	}

	objects, truncated, err := client.BatchObjects(t.Context(), []object.Hash{blobHash}, nil, 10)
	if err != nil {
		t.Fatalf("BatchObjects: %v", err)
	}
	if truncated || len(objects) != 1 || objects[0].Hash != blobHash {
		t.Fatalf("batch objects = %#v truncated=%t, want one non-truncated %s", objects, truncated, blobHash)
	}
	packObjects, packTruncated, err := client.BatchObjectsPack(t.Context(), []object.Hash{blobHash}, nil, 10)
	if err != nil {
		t.Fatalf("BatchObjectsPack: %v", err)
	}
	if packTruncated || len(packObjects) != 1 || packObjects[0].Hash != blobHash {
		t.Fatalf("pack batch objects = %#v truncated=%t, want one non-truncated %s", packObjects, packTruncated, blobHash)
	}

	if err := client.PushObjects(t.Context(), []ObjectRecord{{Type: object.TypeBlob, Data: blobData}}); err != nil {
		t.Fatalf("PushObjects: %v", err)
	}
	if err := client.PushObjectsPack(t.Context(), []ObjectRecord{{Type: object.TypeBlob, Data: blobData}}); err != nil {
		t.Fatalf("PushObjectsPack: %v", err)
	}
	if _, err := client.PushObjectsPackResumable(t.Context(), []ObjectRecord{{Type: object.TypeBlob, Data: blobData}}, ResumablePackUploadOptions{ChunkSize: 64}); err != nil {
		t.Fatalf("PushObjectsPackResumable: %v", err)
	}

	updated, err := client.UpdateRefs(t.Context(), []RefUpdate{{Name: "heads/main", New: &updatedHash}})
	if err != nil {
		t.Fatalf("UpdateRefs: %v", err)
	}
	if updated["heads/main"] != updatedHash {
		t.Fatalf("updated heads/main = %s, want %s", updated["heads/main"], updatedHash)
	}

	for _, key := range []string{"GET refs", "GET object", "POST batch json", "POST batch pack", "POST objects ndjson", "POST objects pack", "POST refs"} {
		if seen[key] != 1 {
			t.Fatalf("%s seen %d time(s), want 1; all seen=%v", key, seen[key], seen)
		}
	}
	if seen["POST objects resumable"] == 0 {
		t.Fatalf("POST objects resumable was not exercised; all seen=%v", seen)
	}
	for _, endpoint := range contract.Endpoints {
		if endpoint.Scope != "repository" {
			continue
		}
		key := endpoint.Method + " " + endpoint.Path
		if seenContractEndpoints[key] == 0 {
			t.Fatalf("repository contract endpoint %s was not exercised; seen=%v", key, seenContractEndpoints)
		}
	}
}

func TestListRefsPaginationLimitExceeded(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"refs":   map[string]string{},
			"cursor": "again",
		})
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListRefs(t.Context())
	if !errors.Is(err, ErrRemotePaginationLimitExceeded) {
		t.Fatalf("error = %v, want ErrRemotePaginationLimitExceeded", err)
	}
	if calls != listRefsPageLimit {
		t.Fatalf("calls = %d, want %d", calls, listRefsPageLimit)
	}
}

func TestPushObjectsPackRoundTrip(t *testing.T) {
	blobData := object.MarshalBlob(&object.Blob{Data: []byte("pack-push-test\n")})
	wantHash := object.HashObject(object.TypeBlob, blobData)

	var serverRecords []ObjectRecord
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graft/alice/repo/objects" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/x-graft-pack" {
			t.Errorf("Content-Type = %q, want application/x-graft-pack", ct)
		}
		ce := r.Header.Get("Content-Encoding")
		if ce != "zstd" {
			t.Errorf("Content-Encoding = %q, want zstd", ce)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		packData, err := decompressZstd(body)
		if err != nil {
			http.Error(w, "decompress: "+err.Error(), http.StatusBadRequest)
			return
		}

		records, err := DecodePackTransport(packData)
		if err != nil {
			http.Error(w, "decode: "+err.Error(), http.StatusBadRequest)
			return
		}
		serverRecords = records

		w.Header().Set("Content-Type", "application/json")
		resp := fmt.Sprintf(`{"received":%d}`, len(records))
		_, _ = w.Write([]byte(resp))
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}

	err = client.PushObjectsPack(t.Context(), []ObjectRecord{
		{Type: object.TypeBlob, Data: blobData},
	})
	if err != nil {
		t.Fatalf("PushObjectsPack: %v", err)
	}

	if len(serverRecords) != 1 {
		t.Fatalf("server received %d objects, want 1", len(serverRecords))
	}
	rec := serverRecords[0]
	if rec.Hash != wantHash {
		t.Fatalf("hash = %s, want %s", rec.Hash, wantHash)
	}
	if rec.Type != object.TypeBlob {
		t.Fatalf("type = %s, want blob", rec.Type)
	}
}

func TestPushObjectsPackResumableChunksAndRetryToken(t *testing.T) {
	source := make([]byte, 4096)
	for i := range source {
		source[i] = byte((i*37 + i/7) % 251)
	}
	blobData := object.MarshalBlob(&object.Blob{Data: source})
	wantHash := object.HashObject(object.TypeBlob, blobData)

	var chunks [][]byte
	var packHash string
	var previousToken string
	var uploadComplete bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graft/alice/repo/objects/resumable" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Content-Type") != "application/x-graft-pack-chunk" {
			t.Errorf("Content-Type = %q, want application/x-graft-pack-chunk", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Content-Encoding") != "zstd" {
			t.Errorf("Content-Encoding = %q, want zstd", r.Header.Get("Content-Encoding"))
		}
		index, err := strconv.Atoi(r.Header.Get("Graft-Chunk-Index"))
		if err != nil {
			http.Error(w, "bad chunk index", http.StatusBadRequest)
			return
		}
		count, err := strconv.Atoi(r.Header.Get("Graft-Chunk-Count"))
		if err != nil || count < 2 {
			http.Error(w, "bad chunk count", http.StatusBadRequest)
			return
		}
		offset, err := strconv.Atoi(r.Header.Get("Graft-Chunk-Offset"))
		if err != nil || offset < 0 {
			http.Error(w, "bad chunk offset", http.StatusBadRequest)
			return
		}
		if index == 0 {
			if got := r.Header.Get("Graft-Retry-Token"); got != "resume-token-0" {
				t.Errorf("first retry token = %q, want resume-token-0", got)
			}
		} else if got := r.Header.Get("Graft-Retry-Token"); got != previousToken {
			t.Errorf("chunk %d retry token = %q, want %q", index, got, previousToken)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if got, want := sha256Hex(body), r.Header.Get("Graft-Chunk-SHA256"); got != want {
			t.Errorf("chunk hash = %q, want %q", got, want)
		}
		if packHash == "" {
			packHash = r.Header.Get("Graft-Pack-SHA256")
		} else if got := r.Header.Get("Graft-Pack-SHA256"); got != packHash {
			t.Errorf("pack hash changed: %q then %q", packHash, got)
		}
		if len(chunks) == 0 {
			chunks = make([][]byte, count)
		}
		if index < 0 || index >= len(chunks) {
			http.Error(w, "chunk index out of range", http.StatusBadRequest)
			return
		}
		if offset != totalBytes(chunks[:index]) {
			t.Errorf("chunk %d offset = %d, want %d", index, offset, totalBytes(chunks[:index]))
		}
		chunks[index] = append([]byte(nil), body...)

		previousToken = fmt.Sprintf("resume-token-%d", index+1)
		complete := index == count-1
		if complete {
			uploadComplete = true
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"upload_id":   "upload-123",
			"retry_token": previousToken,
			"received":    len(body),
			"complete":    complete,
		})
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.PushObjectsPackResumable(
		t.Context(),
		[]ObjectRecord{{Type: object.TypeBlob, Data: blobData}},
		ResumablePackUploadOptions{ChunkSize: 64, RetryToken: "resume-token-0"},
	)
	if err != nil {
		t.Fatalf("PushObjectsPackResumable: %v", err)
	}
	if result.UploadID != "upload-123" || result.RetryToken != previousToken || result.Chunks != len(chunks) || result.Bytes == 0 {
		t.Fatalf("result = %+v previousToken=%q chunks=%d", result, previousToken, len(chunks))
	}
	if !uploadComplete {
		t.Fatal("server did not receive a complete upload")
	}

	var compressed []byte
	for _, chunk := range chunks {
		compressed = append(compressed, chunk...)
	}
	if got := sha256Hex(compressed); got != packHash {
		t.Fatalf("assembled pack hash = %q, want %q", got, packHash)
	}
	pack, err := decompressZstd(compressed)
	if err != nil {
		t.Fatalf("decompressZstd: %v", err)
	}
	records, err := DecodePackTransport(pack)
	if err != nil {
		t.Fatalf("DecodePackTransport: %v", err)
	}
	if len(records) != 1 || records[0].Hash != wantHash || records[0].Type != object.TypeBlob || string(records[0].Data) != string(blobData) {
		t.Fatalf("records = %#v, want %s/%s", records, wantHash, object.TypeBlob)
	}
}

func TestPushObjectsPackResumableRequiresFinalComplete(t *testing.T) {
	blobData := object.MarshalBlob(&object.Blob{Data: []byte("incomplete resumable upload\n")})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graft/alice/repo/objects/resumable" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"upload_id": "upload-123",
			"received":  len(body),
			"complete":  false,
		})
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.PushObjectsPackResumable(
		t.Context(),
		[]ObjectRecord{{Type: object.TypeBlob, Data: blobData}},
		ResumablePackUploadOptions{ChunkSize: 1024},
	)
	if err == nil {
		t.Fatal("PushObjectsPackResumable succeeded, want incomplete upload error")
	}
	if !strings.Contains(err.Error(), "incomplete after final chunk") {
		t.Fatalf("error = %v, want incomplete final chunk error", err)
	}
}

func totalBytes(chunks [][]byte) int {
	var total int
	for _, chunk := range chunks {
		total += len(chunk)
	}
	return total
}

func TestReadAllLimitedRejectsOversizedResponse(t *testing.T) {
	_, err := readAllLimited(strings.NewReader("abcdef"), 5)
	if !errors.Is(err, ErrRemoteResponseTooLarge) {
		t.Fatalf("error = %v, want ErrRemoteResponseTooLarge", err)
	}
}

func TestReadAllLimitedAllowsExactLimit(t *testing.T) {
	got, err := readAllLimited(strings.NewReader("abcde"), 5)
	if err != nil {
		t.Fatalf("readAllLimited: %v", err)
	}
	if string(got) != "abcde" {
		t.Fatalf("body = %q, want abcde", string(got))
	}
}

func TestGetObjectRejectsInvalidHashBeforeRequest(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetObject(t.Context(), object.Hash("bad/hash"))
	if err == nil {
		t.Fatal("expected invalid hash error")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestBatchObjectsRejectsInvalidWantHashBeforeRequest(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.BatchObjects(t.Context(), []object.Hash{"not-a-hash"}, nil, 1)
	if err == nil {
		t.Fatal("expected invalid hash error")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestBatchObjectsPackRejectsInvalidShallowHashBeforeRequest(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.BatchObjectsPackShallow(
		t.Context(),
		[]object.Hash{object.Hash(strings.Repeat("a", 64))},
		nil,
		1,
		&ShallowFetchOpts{Shallow: []object.Hash{"not-a-hash"}},
	)
	if err == nil {
		t.Fatal("expected invalid shallow hash error")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestBatchObjectsPackRejectsInvalidShallowHeader(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Shallow", "not-a-hash")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"objects": []any{},
		})
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.BatchObjectsPackShallow(
		t.Context(),
		[]object.Hash{object.Hash(strings.Repeat("a", 64))},
		nil,
		1,
		nil,
	)
	if err == nil {
		t.Fatal("expected invalid X-Shallow hash error")
	}
}

func TestUpdateRefsRejectsInvalidHashBeforeRequest(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	bad := object.Hash("not-a-hash")
	_, err = client.UpdateRefs(t.Context(), []RefUpdate{{
		Name: "heads/main",
		New:  &bad,
	}})
	if err == nil {
		t.Fatal("expected invalid hash error")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestUpdateRefsPreservesCASConflictFromMockOrchard(t *testing.T) {
	current := object.Hash(strings.Repeat("a", 64))
	staleOld := object.Hash(strings.Repeat("b", 64))
	newHash := object.Hash(strings.Repeat("c", 64))
	updated := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graft/alice/repo/refs" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req struct {
			Updates []struct {
				Name string `json:"name"`
				Old  string `json:"old"`
				New  string `json:"new"`
			} `json:"updates"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Updates) != 1 {
			t.Errorf("updates = %#v, want one update", req.Updates)
			http.Error(w, "bad update count", http.StatusBadRequest)
			return
		}
		update := req.Updates[0]
		if update.Name != "heads/main" || update.Old != string(staleOld) || update.New != string(newHash) {
			t.Errorf("update = %#v, want heads/main old=%s new=%s", update, staleOld, newHash)
			http.Error(w, "bad update payload", http.StatusBadRequest)
			return
		}
		if update.Old != string(current) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(RemoteError{
				Code:    "ref_cas_mismatch",
				Message: "ref update conflict",
				Detail:  "heads/main changed on remote; fetch and retry",
			})
			return
		}
		updated = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"updated": map[string]string{"heads/main": string(newHash)},
		})
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.UpdateRefs(t.Context(), []RefUpdate{{
		Name: "heads/main",
		Old:  &staleOld,
		New:  &newHash,
	}})
	if err == nil {
		t.Fatal("UpdateRefs succeeded, want CAS conflict")
	}
	var remoteErr *RemoteError
	if !errors.As(err, &remoteErr) {
		t.Fatalf("error = %T %[1]v, want *RemoteError", err)
	}
	if remoteErr.Code != "ref_cas_mismatch" {
		t.Fatalf("remote error code = %q, want ref_cas_mismatch", remoteErr.Code)
	}
	if updated {
		t.Fatal("mock server applied update despite CAS mismatch")
	}
}

func TestPushObjectsPackReturnsUnsupportedErrorForLegacyServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}

	blobData := object.MarshalBlob(&object.Blob{Data: []byte("pack-push-test\n")})
	err = client.PushObjectsPack(t.Context(), []ObjectRecord{
		{Type: object.TypeBlob, Data: blobData},
	})
	if err == nil {
		t.Fatal("expected pack upload unsupported error")
	}
	if !IsPackUploadUnsupported(err) {
		t.Fatalf("expected IsPackUploadUnsupported(err) = true, got %v", err)
	}
}

func TestClientCachesServerCapabilities(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Graft-Capabilities", "pack,zstd")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"heads/main": strings.Repeat("a", 64),
		})
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	if client.ServerCapabilities() != nil {
		t.Fatal("expected nil capabilities before any request")
	}
	if _, err := client.ListRefs(t.Context()); err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	caps := client.ServerCapabilities()
	if caps == nil {
		t.Fatal("expected capabilities to be cached after request")
	}
	if !caps.Has(CapPack) || !caps.Has(CapZstd) {
		t.Fatalf("unexpected capabilities: %s", caps.String())
	}
}

func TestClientCachesServerLimits(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Graft-Limits", "max_batch=5000,max_payload=10000000")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	if client.ServerLimits() != nil {
		t.Fatal("expected nil limits before any request")
	}
	_, _ = client.ListRefs(t.Context())
	limits := client.ServerLimits()
	if limits == nil {
		t.Fatal("expected limits to be cached after request")
	}
	if limits.MaxBatch != 5000 {
		t.Fatalf("MaxBatch = %d, want 5000", limits.MaxBatch)
	}
	if limits.MaxPayload != 10000000 {
		t.Fatalf("MaxPayload = %d, want 10000000", limits.MaxPayload)
	}
}

func TestBatchObjectsHonorsCachedMaxBatch(t *testing.T) {
	var gotMaxObjects int
	var requests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graft/alice/repo/objects/batch" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		requests++
		var req struct {
			MaxObjects int `json:"max_objects"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotMaxObjects = req.MaxObjects
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"objects":   []any{},
			"truncated": false,
		})
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	client.serverLimits = &ServerLimits{MaxBatch: 2}

	_, truncated, err := client.BatchObjects(t.Context(), []object.Hash{object.Hash(strings.Repeat("a", 64))}, nil, 100)
	if err != nil {
		t.Fatalf("BatchObjects: %v", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false")
	}
	if gotMaxObjects != 2 {
		t.Fatalf("max_objects = %d, want 2", gotMaxObjects)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestBatchObjectsRejectsCachedMaxPayloadBeforeRequest(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	client.serverLimits = &ServerLimits{MaxPayload: 10}

	_, _, err = client.BatchObjects(t.Context(), []object.Hash{object.Hash(strings.Repeat("a", 64))}, nil, 1)
	if !errors.Is(err, ErrRemoteLimitExceeded) {
		t.Fatalf("error = %v, want ErrRemoteLimitExceeded", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestBatchObjectsRejectsInvalidHaveBeforePayloadTrim(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	client.serverLimits = &ServerLimits{MaxPayload: 600}

	_, _, err = client.BatchObjects(
		t.Context(),
		[]object.Hash{object.Hash(strings.Repeat("a", 64))},
		[]object.Hash{
			object.Hash("not-a-hash"),
			object.Hash(strings.Repeat("b", 64)),
		},
		1,
	)
	if err == nil {
		t.Fatal("expected invalid have hash error")
	}
	if !strings.Contains(err.Error(), "invalid have hash") {
		t.Fatalf("error = %v, want invalid have hash", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestPushObjectsRejectsCachedMaxObjectBeforeRequest(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	client.serverLimits = &ServerLimits{MaxObject: 4}

	blobData := object.MarshalBlob(&object.Blob{Data: []byte("hello\n")})
	err = client.PushObjects(t.Context(), []ObjectRecord{
		{Type: object.TypeBlob, Data: blobData},
	})
	if !errors.Is(err, ErrRemoteLimitExceeded) {
		t.Fatalf("error = %v, want ErrRemoteLimitExceeded", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestPushObjectsRejectsCachedMaxBatchBeforeRequest(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	client.serverLimits = &ServerLimits{MaxBatch: 1}

	err = client.PushObjects(t.Context(), []ObjectRecord{
		{Type: object.TypeBlob, Data: object.MarshalBlob(&object.Blob{Data: []byte("a")})},
		{Type: object.TypeBlob, Data: object.MarshalBlob(&object.Blob{Data: []byte("b")})},
	})
	if !errors.Is(err, ErrRemoteLimitExceeded) {
		t.Fatalf("error = %v, want ErrRemoteLimitExceeded", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestGetObjectHonorsAdvertisedMaxObjectResponseLimit(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/graft/alice/repo/objects/") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		requests++
		w.Header().Set("Graft-Limits", "max_object=5")
		w.Header().Set("X-Object-Type", string(object.TypeBlob))
		_, _ = w.Write([]byte("123456"))
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetObject(t.Context(), object.Hash(strings.Repeat("a", 64)))
	if !errors.Is(err, ErrRemoteResponseTooLarge) {
		t.Fatalf("error = %v, want ErrRemoteResponseTooLarge", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}
