package coord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
)

// fakeOrchard is an in-process stand-in for the graft protocol server side. It
// persists pushed objects + refs in memory and serves them back, so a real
// remote.Client round-trips end to end. It deliberately serves objects ONLY
// from what was actually pushed (no peer-store shortcut) so a transport that
// drops the feed chain cannot be masked by the harness.
type fakeOrchard struct {
	mu       sync.Mutex
	store    *object.Store          // backing object store
	refs     map[string]object.Hash // bare names: "coord/feed/head" -> hash
	objPosts int                    // count of /objects POSTs (push calls)
	// casFail, if set, forces a single 409 ref_conflict for names it returns
	// true for, to drive the optimistic-concurrency retry path in tests.
	casFail func(name string) bool
}

type refUpdateWire struct {
	Name string  `json:"name"`
	Old  *string `json:"old,omitempty"`
	New  *string `json:"new"`
}

const fakeBasePath = "/graft/alice/repo"

// newFakeOrchard returns a started fake server; caller defers ts.Close().
func newFakeOrchard(t *testing.T) (*fakeOrchard, *httptest.Server) {
	t.Helper()
	f := &fakeOrchard{
		store: object.NewStore(t.TempDir()),
		refs:  map[string]object.Hash{},
	}
	ts := httptest.NewServer(http.HandlerFunc(f.handle))
	return f, ts
}

// baseURL is the client base URL for this fake (NewClient(... + baseURL)).
func (f *fakeOrchard) baseURL(ts *httptest.Server) string { return ts.URL + fakeBasePath }

func (f *fakeOrchard) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == fakeBasePath+"/objects":
		f.handlePushObjects(w, r)
	case r.Method == http.MethodPost && r.URL.Path == fakeBasePath+"/refs":
		f.handleUpdateRefs(w, r)
	case r.Method == http.MethodGet && r.URL.Path == fakeBasePath+"/refs":
		f.handleListRefs(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, fakeBasePath+"/objects/"):
		f.handleGetObject(w, r)
	default:
		http.Error(w, "not found: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}
}

func (f *fakeOrchard) handlePushObjects(w http.ResponseWriter, r *http.Request) {
	// Only the ndjson push path is supported; reject pack so the client falls
	// back to ndjson (never silently drops objects).
	if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "x-ndjson") {
		http.Error(w, `{"code":"pack_unsupported","error":"pack upload not supported"}`, http.StatusUnsupportedMediaType)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objPosts++
	dec := json.NewDecoder(r.Body)
	for {
		var rec struct {
			Hash string `json:"hash"`
			Type string `json:"type"`
			Data []byte `json:"data"`
		}
		if err := dec.Decode(&rec); err == io.EOF {
			break
		} else if err != nil {
			http.Error(w, "bad ndjson", http.StatusBadRequest)
			return
		}
		if _, err := f.store.Write(object.ObjectType(rec.Type), rec.Data); err != nil {
			http.Error(w, "store write failed", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{}`))
}

func (f *fakeOrchard) handleUpdateRefs(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var req struct {
		Updates []refUpdateWire `json:"updates"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad refs body", http.StatusBadRequest)
		return
	}
	updated := map[string]string{}
	for _, u := range req.Updates {
		if f.casFail != nil && f.casFail(u.Name) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"ref_conflict","error":"compare-and-swap failed"}`))
			return
		}
		expectedOld := ""
		if u.Old != nil {
			expectedOld = strings.TrimSpace(*u.Old)
		}
		current := string(f.refs[u.Name]) // "" if absent
		if current != expectedOld {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"ref_conflict","error":"compare-and-swap failed"}`))
			return
		}
		newVal := ""
		if u.New != nil {
			newVal = strings.TrimSpace(*u.New)
		}
		if newVal == "" {
			delete(f.refs, u.Name)
		} else {
			f.refs[u.Name] = object.Hash(newVal)
		}
		updated[u.Name] = newVal
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"updated": updated})
}

func (f *fakeOrchard) handleListRefs(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for name, hash := range f.refs {
		out[name] = string(hash)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"refs": out})
}

func (f *fakeOrchard) handleGetObject(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := object.Hash(path.Base(r.URL.Path))
	objType, data, err := f.store.Read(h)
	if err != nil {
		http.Error(w, "object not found", http.StatusNotFound)
		return
	}
	w.Header().Set("X-Object-Type", string(objType))
	_, _ = w.Write(data)
}

// hasObject reports whether the fake's store holds the given hash (test assertion helper).
func (f *fakeOrchard) hasObject(h object.Hash) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.store.Has(h)
}

// refHash returns the remote value of a coord ref (empty if absent).
func (f *fakeOrchard) refHash(name string) object.Hash {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refs[name]
}

// TestFakeOrchard_RoundTrip is the harness smoke test: a real remote.Client
// pushes an object, CAS-updates a ref to it, lists the ref back, and fetches
// the object — proving the fake faithfully implements the wire contract.
func TestFakeOrchard_RoundTrip(t *testing.T) {
	f, ts := newFakeOrchard(t)
	defer ts.Close()

	client, err := remote.NewClient(f.baseURL(ts))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	data := []byte("hello coord\n")
	h := object.HashObject(object.TypeBlob, data)
	if err := client.PushObjects(ctx, []remote.ObjectRecord{{Type: object.TypeBlob, Data: data}}); err != nil {
		t.Fatalf("PushObjects: %v", err)
	}
	if !f.hasObject(h) {
		t.Fatalf("pushed object %s not in fake store", h)
	}

	newHash := h
	if _, err := client.UpdateRefs(ctx, []remote.RefUpdate{{Name: "coord/feed/head", New: &newHash}}); err != nil {
		t.Fatalf("UpdateRefs: %v", err)
	}
	refs, err := client.ListRefs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if refs["coord/feed/head"] != h {
		t.Fatalf("ListRefs[coord/feed/head] = %q, want %q", refs["coord/feed/head"], h)
	}
}
