package remote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/userconfig"
)

// Endpoint identifies a Graft protocol repository endpoint.
// BaseURL is normalized to ".../graft/{owner}/{repo}" with no trailing slash.
type Endpoint struct {
	Raw     string
	BaseURL string
	Owner   string
	Repo    string
	user    string
	pass    string
}

// ParseEndpoint parses a remote URL into a canonical endpoint.
//
// Supported inputs include:
// - https://host/graft/owner/repo
// - https://host/owner/repo (expanded to /graft/owner/repo)
// - https://host/api/v1/graft/owner/repo
func ParseEndpoint(raw string) (Endpoint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Endpoint{}, fmt.Errorf("remote URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Endpoint{}, fmt.Errorf("parse remote URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return Endpoint{}, fmt.Errorf("remote URL must include scheme and host")
	}

	segments := splitPathSegments(u.Path)
	if len(segments) < 2 {
		return Endpoint{}, fmt.Errorf("remote URL must include owner and repository")
	}

	graftIdx := -1
	for i := 0; i+2 < len(segments); i++ {
		if segments[i] == "graft" {
			graftIdx = i
		}
	}

	var owner, repo string
	var baseSegments []string
	if graftIdx >= 0 {
		owner = segments[graftIdx+1]
		repo = segments[graftIdx+2]
		baseSegments = append(baseSegments, segments[:graftIdx+3]...)
	} else {
		owner = segments[len(segments)-2]
		repo = segments[len(segments)-1]
		baseSegments = append(baseSegments, segments[:len(segments)-2]...)
		baseSegments = append(baseSegments, "graft", owner, repo)
	}
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(repo) == "" {
		return Endpoint{}, fmt.Errorf("remote URL must include non-empty owner and repository")
	}

	endpointURL := *u
	endpointURL.Path = "/" + strings.Join(baseSegments, "/")
	endpointURL.RawPath = ""
	endpointURL.RawQuery = ""
	endpointURL.Fragment = ""
	user := ""
	pass := ""
	if endpointURL.User != nil {
		user = endpointURL.User.Username()
		pass, _ = endpointURL.User.Password()
	}
	endpointURL.User = nil

	return Endpoint{
		Raw:     raw,
		BaseURL: strings.TrimRight(endpointURL.String(), "/"),
		Owner:   owner,
		Repo:    repo,
		user:    user,
		pass:    pass,
	}, nil
}

func splitPathSegments(p string) []string {
	p = strings.TrimSpace(path.Clean(p))
	p = strings.TrimPrefix(p, "/")
	if p == "" || p == "." {
		return nil
	}
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && part != "." {
			out = append(out, part)
		}
	}
	return out
}

// ObjectRecord is an object payload used by push/pull operations.
type ObjectRecord struct {
	Hash object.Hash
	Type object.ObjectType
	Data []byte
}

// ShallowFetchOpts controls shallow/depth-related fields in a batch request.
type ShallowFetchOpts struct {
	Depth   int           // shallow clone depth (0 = full)
	Deepen  int           // deepen by N commits (0 = no deepen)
	Shallow []object.Hash // existing shallow boundary hashes
	Filter  string        // partial clone filter spec
}

// RefUpdate is one atomic reference update request.
type RefUpdate struct {
	Name string
	Old  *object.Hash
	New  *object.Hash
}

// ResumablePackUploadOptions controls chunked pack upload.
type ResumablePackUploadOptions struct {
	ChunkSize  int
	RetryToken string
}

// ResumablePackUploadResult describes a completed resumable pack upload.
type ResumablePackUploadResult struct {
	UploadID   string
	RetryToken string
	Chunks     int
	Bytes      int
}

// ClientOptions configures the remote protocol client.
type ClientOptions struct {
	Timeout     time.Duration // HTTP client timeout (default 60s)
	MaxAttempts int           // retry attempts (default 3)
}

// Response limits per endpoint type.
const (
	responseLimitDefault = 2 << 20  // 2MB
	responseLimitRefs    = 8 << 20  // 8MB
	responseLimitBatch   = 64 << 20 // 64MB
	responseLimitObject  = 32 << 20 // 32MB

	listRefsPageLimit = 1024

	defaultResumablePackChunkSize = 4 << 20
	maxResumablePackChunkSize     = 64 << 20
)

// Client is a transport client for orchard's Graft protocol.
type Client struct {
	endpoint     Endpoint
	httpClient   *http.Client
	token        string
	user         string
	pass         string
	maxAttempts  int
	serverLimits *ServerLimits
	serverCaps   *Capabilities
}

// ErrPackUploadUnsupported indicates the remote does not accept pack uploads.
var ErrPackUploadUnsupported = errors.New("pack upload unsupported")

// ErrRemoteResponseTooLarge indicates a remote response exceeded the client's
// configured byte limit before it could be decoded safely.
var ErrRemoteResponseTooLarge = errors.New("remote response exceeds byte limit")

// ErrRemotePaginationLimitExceeded indicates a paginated remote endpoint did
// not terminate within the client's page limit.
var ErrRemotePaginationLimitExceeded = errors.New("remote pagination exceeded page limit")

// ErrRemoteLimitExceeded indicates a local request would exceed limits
// advertised by the remote server.
var ErrRemoteLimitExceeded = errors.New("remote limit exceeded")

// NewClient creates a remote protocol client with default options.
//
// Auth resolution order:
// 1) GRAFT_TOKEN (Bearer)
// 2) ~/.graftconfig host-matching Orchard profile token (Bearer)
// 3) GRAFT_USERNAME + GRAFT_PASSWORD (Basic)
// 4) URL userinfo (Basic)
func NewClient(remoteURL string) (*Client, error) {
	return NewClientWithOptions(remoteURL, ClientOptions{})
}

// NewClientWithOptions creates a remote protocol client with configurable options.
// Zero-value or negative fields in opts receive defaults (60s timeout, 3 attempts).
func NewClientWithOptions(remoteURL string, opts ClientOptions) (*Client, error) {
	endpoint, err := ParseEndpoint(remoteURL)
	if err != nil {
		return nil, err
	}

	if opts.Timeout <= 0 {
		opts.Timeout = 60 * time.Second
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 3
	}

	token := strings.TrimSpace(os.Getenv("GRAFT_TOKEN"))
	user := strings.TrimSpace(os.Getenv("GRAFT_USERNAME"))
	pass := os.Getenv("GRAFT_PASSWORD")
	if token == "" {
		if cfg, err := userconfig.Load(); err == nil {
			token = strings.TrimSpace(cfg.OrchardProfile(endpoint.OrchardBaseURL()).Token)
		}
	}
	if token == "" && user == "" && endpoint.user != "" {
		user = endpoint.user
		pass = endpoint.pass
	}

	return &Client{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: opts.Timeout,
		},
		token:       token,
		user:        user,
		pass:        pass,
		maxAttempts: opts.MaxAttempts,
	}, nil
}

// Endpoint returns the parsed endpoint metadata.
func (c *Client) Endpoint() Endpoint {
	return c.endpoint
}

// OrchardBaseURL returns the Orchard server base URL for this endpoint, with the
// trailing /graft/{owner}/{repo} path removed.
func (e Endpoint) OrchardBaseURL() string {
	u, err := url.Parse(e.BaseURL)
	if err != nil {
		return ""
	}
	segments := splitPathSegments(u.Path)
	for i := 0; i+2 < len(segments); i++ {
		if segments[i] != "graft" {
			continue
		}
		segments = segments[:i]
		break
	}
	if len(segments) == 0 {
		u.Path = ""
	} else {
		u.Path = "/" + strings.Join(segments, "/")
	}
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	return strings.TrimRight(u.String(), "/")
}

// ServerLimits returns the cached server-advertised limits, or nil if not yet received.
func (c *Client) ServerLimits() *ServerLimits {
	return c.serverLimits
}

// ServerCapabilities returns the cached server-advertised capabilities, or nil
// if the server has not advertised any yet.
func (c *Client) ServerCapabilities() *Capabilities {
	return c.serverCaps
}

func (c *Client) cacheServerMetadata(resp *http.Response) {
	c.cacheServerLimits(resp)
	c.cacheServerCapabilities(resp)
}

func (c *Client) cacheServerLimits(resp *http.Response) {
	if c.serverLimits != nil {
		return // already cached
	}
	raw := resp.Header.Get(headerLimits)
	if raw == "" {
		return
	}
	limits := ParseLimits(raw)
	c.serverLimits = &limits
}

func (c *Client) cacheServerCapabilities(resp *http.Response) {
	if c.serverCaps != nil {
		return
	}
	raw := strings.TrimSpace(resp.Header.Get(headerCapabilities))
	if raw == "" {
		return
	}
	caps := ParseCapabilities(raw)
	c.serverCaps = &caps
}

// ListRefs returns all remote refs (e.g. heads/main, tags/v1).
// It supports paginated responses: if the server includes a "cursor" field,
// the client loops with ?cursor=X&limit=1000 until no cursor is returned.
// Legacy flat-map responses (no "refs" wrapper) are handled as a single page.
func (c *Client) ListRefs(ctx context.Context) (map[string]object.Hash, error) {
	refs := make(map[string]object.Hash)
	cursor := ""
	const pageLimit = 1000

	for page := 0; page < listRefsPageLimit; page++ {
		u := c.endpoint.BaseURL + "/refs"
		if cursor != "" {
			u += "?cursor=" + url.QueryEscape(cursor) + "&limit=" + fmt.Sprintf("%d", pageLimit)
		} else {
			u += "?limit=" + fmt.Sprintf("%d", pageLimit)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		body, err := c.doWithLimit(req, http.StatusOK, responseLimitRefs, "application/json")
		if err != nil {
			return nil, err
		}

		var page struct {
			Refs   map[string]string `json:"refs"`
			Cursor string            `json:"cursor"`
		}
		// Try paginated format first.
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode refs response: %w", err)
		}

		// If the response has a "refs" key, it's paginated format.
		// If not, it's the legacy flat map format.
		refMap := page.Refs
		if refMap == nil {
			// Legacy format: the entire response is a flat map[string]string
			var raw map[string]string
			if err := json.Unmarshal(body, &raw); err != nil {
				return nil, fmt.Errorf("decode refs response: %w", err)
			}
			refMap = raw
		}

		for name, hash := range refMap {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			h := object.Hash(strings.TrimSpace(hash))
			if err := ValidateHash(h); err != nil {
				return nil, fmt.Errorf("invalid hash for ref %q: %w", name, err)
			}
			refs[name] = h
		}

		if page.Cursor == "" || page.Refs == nil {
			return refs, nil
		}
		cursor = page.Cursor
	}

	return nil, fmt.Errorf("%w: limit=%d", ErrRemotePaginationLimitExceeded, listRefsPageLimit)
}

// BatchObjects fetches missing objects reachable from wants and not in haves.
func (c *Client) BatchObjects(ctx context.Context, wants, haves []object.Hash, maxObjects int) ([]ObjectRecord, bool, error) {
	if len(wants) == 0 {
		return nil, false, fmt.Errorf("at least one want hash is required")
	}
	maxObjects = c.effectiveMaxBatchObjects(maxObjects)

	validatedWants, err := validateHashList(wants, "want")
	if err != nil {
		return nil, false, err
	}
	validatedHaves, err := validateHashList(haves, "have")
	if err != nil {
		return nil, false, err
	}
	validatedHaves = c.limitHaveHashesForPayload(validatedHaves)

	reqBody := struct {
		Wants      []string `json:"wants"`
		Haves      []string `json:"haves,omitempty"`
		MaxObjects int      `json:"max_objects,omitempty"`
	}{
		Wants:      hashStrings(validatedWants),
		Haves:      hashStrings(validatedHaves),
		MaxObjects: maxObjects,
	}
	if len(reqBody.Wants) == 0 {
		return nil, false, fmt.Errorf("at least one non-empty want hash is required")
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, false, err
	}
	if err := c.checkPayloadLimit(len(payload), "batch object request"); err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.BaseURL+"/objects/batch", bytes.NewReader(payload))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")

	body, err := c.doWithLimit(req, http.StatusOK, responseLimitBatch, "application/json")
	if err != nil {
		return nil, false, err
	}

	var resp struct {
		Objects []struct {
			Hash string `json:"hash"`
			Type string `json:"type"`
			Data []byte `json:"data"`
		} `json:"objects"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, false, fmt.Errorf("decode batch response: %w", err)
	}

	out := make([]ObjectRecord, 0, len(resp.Objects))
	for _, obj := range resp.Objects {
		objType, err := parseObjectType(obj.Type)
		if err != nil {
			return nil, false, err
		}
		h := object.Hash(strings.TrimSpace(obj.Hash))
		if err := ValidateHash(h); err != nil {
			return nil, false, fmt.Errorf("invalid hash in batch response: %w", err)
		}
		out = append(out, ObjectRecord{
			Hash: h,
			Type: objType,
			Data: obj.Data,
		})
	}
	return out, resp.Truncated, nil
}

// BatchShallowResult holds the batch objects along with any shallow boundary
// hashes advertised by the server in the response.
type BatchShallowResult struct {
	Objects   []ObjectRecord
	Truncated bool
	Shallow   []object.Hash // shallow boundary hashes from server
}

// BatchObjectsPack fetches missing objects using pack transport with optional
// zstd compression. It sends Accept: application/x-graft-pack to request pack
// encoding, but falls back to JSON decoding if the server responds with
// application/json content type.
func (c *Client) BatchObjectsPack(ctx context.Context, wants, haves []object.Hash, maxObjects int) ([]ObjectRecord, bool, error) {
	result, err := c.BatchObjectsPackShallow(ctx, wants, haves, maxObjects, nil)
	if err != nil {
		return nil, false, err
	}
	return result.Objects, result.Truncated, nil
}

// BatchObjectsPackShallow is like BatchObjectsPack but accepts shallow options
// and returns shallow boundary hashes from the server response.
func (c *Client) BatchObjectsPackShallow(ctx context.Context, wants, haves []object.Hash, maxObjects int, shallowOpts *ShallowFetchOpts) (*BatchShallowResult, error) {
	if len(wants) == 0 {
		return nil, fmt.Errorf("at least one want hash is required")
	}
	maxObjects = c.effectiveMaxBatchObjects(maxObjects)

	validatedWants, err := validateHashList(wants, "want")
	if err != nil {
		return nil, err
	}
	validatedHaves, err := validateHashList(haves, "have")
	if err != nil {
		return nil, err
	}
	validatedHaves = c.limitHaveHashesForPayload(validatedHaves)

	reqBody := struct {
		Wants      []string `json:"wants"`
		Haves      []string `json:"haves,omitempty"`
		MaxObjects int      `json:"max_objects,omitempty"`
		Depth      int      `json:"depth,omitempty"`
		Deepen     int      `json:"deepen,omitempty"`
		Shallow    []string `json:"shallow,omitempty"`
		Filter     string   `json:"filter,omitempty"`
	}{
		Wants:      hashStrings(validatedWants),
		Haves:      hashStrings(validatedHaves),
		MaxObjects: maxObjects,
	}
	if len(reqBody.Wants) == 0 {
		return nil, fmt.Errorf("at least one non-empty want hash is required")
	}

	if shallowOpts != nil {
		reqBody.Depth = shallowOpts.Depth
		reqBody.Deepen = shallowOpts.Deepen
		reqBody.Filter = shallowOpts.Filter
		validatedShallow, err := validateHashList(shallowOpts.Shallow, "shallow")
		if err != nil {
			return nil, err
		}
		reqBody.Shallow = hashStrings(validatedShallow)
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	if err := c.checkPayloadLimit(len(payload), "batch object request"); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.BaseURL+"/objects/batch", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-graft-pack")
	req.Header.Set("Accept-Encoding", "zstd")
	c.applyAuth(req)

	resp, err := retryDo(c.httpClient, req, c.maxAttempts)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	c.cacheServerMetadata(resp)

	body, readErr := readAllLimited(resp.Body, c.effectiveResponseLimit(responseLimitBatch))
	if readErr != nil {
		return nil, readErr
	}

	if resp.StatusCode != http.StatusOK {
		if re := tryParseRemoteError(body); re != nil {
			return nil, re
		}
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("remote request failed (%s %s): %s", req.Method, req.URL.Path, msg)
	}

	// Parse shallow boundaries from response header.
	var shallowHashes []object.Hash
	if raw := resp.Header.Get("X-Shallow"); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				h := object.Hash(s)
				if err := ValidateHash(h); err != nil {
					return nil, fmt.Errorf("invalid X-Shallow hash: %w", err)
				}
				shallowHashes = append(shallowHashes, h)
			}
		}
	}

	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/x-graft-pack") {
		// Pack transport response: optionally zstd-compressed.
		packData := body
		if isZstdEncoded(resp.Header.Get("Content-Encoding")) {
			packData, err = decompressZstd(body)
			if err != nil {
				return nil, fmt.Errorf("decompress pack response: %w", err)
			}
		}
		records, err := DecodePackTransport(packData)
		if err != nil {
			return nil, fmt.Errorf("decode pack response: %w", err)
		}
		truncated := strings.EqualFold(resp.Header.Get("X-Truncated"), "true")
		return &BatchShallowResult{Objects: records, Truncated: truncated, Shallow: shallowHashes}, nil
	}

	// JSON fallback: server returned application/json.
	var jsonResp struct {
		Objects []struct {
			Hash string `json:"hash"`
			Type string `json:"type"`
			Data []byte `json:"data"`
		} `json:"objects"`
		Truncated bool     `json:"truncated"`
		Shallow   []string `json:"shallow"`
	}
	if err := json.Unmarshal(body, &jsonResp); err != nil {
		return nil, fmt.Errorf("decode batch response: %w", err)
	}

	// Merge shallow boundaries from JSON body with header.
	for _, s := range jsonResp.Shallow {
		s = strings.TrimSpace(s)
		if s != "" {
			h := object.Hash(s)
			if err := ValidateHash(h); err != nil {
				return nil, fmt.Errorf("invalid shallow hash in batch response: %w", err)
			}
			shallowHashes = append(shallowHashes, h)
		}
	}

	out := make([]ObjectRecord, 0, len(jsonResp.Objects))
	for _, obj := range jsonResp.Objects {
		objType, err := parseObjectType(obj.Type)
		if err != nil {
			return nil, err
		}
		h := object.Hash(strings.TrimSpace(obj.Hash))
		if err := ValidateHash(h); err != nil {
			return nil, fmt.Errorf("invalid hash in batch response: %w", err)
		}
		out = append(out, ObjectRecord{
			Hash: h,
			Type: objType,
			Data: obj.Data,
		})
	}
	return &BatchShallowResult{Objects: out, Truncated: jsonResp.Truncated, Shallow: shallowHashes}, nil
}

// GetObject fetches one object by hash.
func (c *Client) GetObject(ctx context.Context, hash object.Hash) (ObjectRecord, error) {
	hash = object.Hash(strings.TrimSpace(string(hash)))
	if hash == "" {
		return ObjectRecord{}, fmt.Errorf("object hash is required")
	}
	if err := ValidateHash(hash); err != nil {
		return ObjectRecord{}, fmt.Errorf("invalid object hash: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint.BaseURL+"/objects/"+string(hash), nil)
	if err != nil {
		return ObjectRecord{}, err
	}
	c.applyAuth(req)

	resp, err := retryDo(c.httpClient, req, c.maxAttempts)
	if err != nil {
		return ObjectRecord{}, err
	}
	defer resp.Body.Close()
	c.cacheServerMetadata(resp)

	body, readErr := readAllLimited(resp.Body, c.effectiveObjectResponseLimit(responseLimitObject))
	if readErr != nil {
		return ObjectRecord{}, readErr
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return ObjectRecord{}, fmt.Errorf("remote request failed (%s %s): %s", req.Method, req.URL.Path, msg)
	}

	objType, err := parseObjectType(strings.TrimSpace(resp.Header.Get("X-Object-Type")))
	if err != nil {
		return ObjectRecord{}, fmt.Errorf("decode object %s: %w", hash, err)
	}
	return ObjectRecord{
		Hash: hash,
		Type: objType,
		Data: body,
	}, nil
}

// PushObjects uploads objects using newline-delimited JSON payload.
func (c *Client) PushObjects(ctx context.Context, objects []ObjectRecord) error {
	if len(objects) == 0 {
		return nil
	}
	if err := c.checkObjectUploadLimits(objects); err != nil {
		return err
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i, obj := range objects {
		if _, err := parseObjectType(string(obj.Type)); err != nil {
			return fmt.Errorf("push object %d: %w", i, err)
		}
		computedHash := object.HashObject(obj.Type, obj.Data)
		if provided := object.Hash(strings.TrimSpace(string(obj.Hash))); provided != "" && provided != computedHash {
			return fmt.Errorf("push object %d: hash mismatch (provided %s, computed %s)", i, provided, computedHash)
		}
		payload := struct {
			Hash string `json:"hash"`
			Type string `json:"type"`
			Data []byte `json:"data"`
		}{
			Hash: string(computedHash),
			Type: string(obj.Type),
			Data: obj.Data,
		}
		if err := enc.Encode(payload); err != nil {
			return fmt.Errorf("push object %d: encode: %w", i, err)
		}
	}
	if err := c.checkPayloadLimit(buf.Len(), "object upload"); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.BaseURL+"/objects", bytes.NewReader(buf.Bytes()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")

	if _, err := c.doWithLimit(req, http.StatusOK, 1<<20, "application/json"); err != nil {
		return err
	}
	return nil
}

// PushObjectsPack uploads objects using zstd-compressed pack transport.
func (c *Client) PushObjectsPack(ctx context.Context, objects []ObjectRecord) error {
	if len(objects) == 0 {
		return nil
	}
	if err := c.checkObjectUploadLimits(objects); err != nil {
		return err
	}

	for i, obj := range objects {
		if _, err := parseObjectType(string(obj.Type)); err != nil {
			return fmt.Errorf("push object %d: %w", i, err)
		}
		computedHash := object.HashObject(obj.Type, obj.Data)
		if provided := object.Hash(strings.TrimSpace(string(obj.Hash))); provided != "" && provided != computedHash {
			return fmt.Errorf("push object %d: hash mismatch (provided %s, computed %s)", i, provided, computedHash)
		}
		objects[i].Hash = computedHash
	}

	packData, err := EncodePackTransportToBytes(objects)
	if err != nil {
		return fmt.Errorf("encode pack: %w", err)
	}

	compressed, err := compressZstd(packData)
	if err != nil {
		return fmt.Errorf("compress pack: %w", err)
	}
	if err := c.checkPayloadLimit(len(compressed), "pack upload"); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.BaseURL+"/objects", bytes.NewReader(compressed))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-graft-pack")
	req.Header.Set("Content-Encoding", "zstd")
	c.applyAuth(req)

	resp, err := retryDo(c.httpClient, req, c.maxAttempts)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	c.cacheServerMetadata(resp)

	body, readErr := readAllLimited(resp.Body, c.effectiveResponseLimit(1<<20))
	if readErr != nil {
		return readErr
	}

	if resp.StatusCode != http.StatusOK {
		if re := tryParseRemoteError(body); re != nil {
			if isPackUploadUnsupportedResponse(resp.StatusCode, re.Error()) {
				return fmt.Errorf("%w: %v", ErrPackUploadUnsupported, re)
			}
			return re
		}
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		if isPackUploadUnsupportedResponse(resp.StatusCode, msg) {
			return fmt.Errorf("%w: remote request failed (%s %s): %s", ErrPackUploadUnsupported, req.Method, req.URL.Path, msg)
		}
		return fmt.Errorf("remote request failed (%s %s): %s", req.Method, req.URL.Path, msg)
	}

	return nil
}

// PushObjectsPackResumable uploads objects using zstd-compressed pack transport
// split into independently hashed chunks. The returned retry token can be used
// by callers to resume after an interrupted upload when the server supports it.
func (c *Client) PushObjectsPackResumable(ctx context.Context, objects []ObjectRecord, opts ResumablePackUploadOptions) (*ResumablePackUploadResult, error) {
	if len(objects) == 0 {
		return &ResumablePackUploadResult{}, nil
	}
	if err := c.checkObjectUploadLimits(objects); err != nil {
		return nil, err
	}

	_, compressed, err := encodeCompressedPackForUpload(objects)
	if err != nil {
		return nil, err
	}

	chunkSize := opts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = defaultResumablePackChunkSize
	}
	if chunkSize > maxResumablePackChunkSize {
		return nil, fmt.Errorf("%w: resumable pack chunk size %d exceeds maximum %d", ErrRemoteLimitExceeded, chunkSize, maxResumablePackChunkSize)
	}
	if limits := c.ServerLimits(); limits != nil && limits.MaxPayload > 0 && chunkSize > limits.MaxPayload {
		chunkSize = limits.MaxPayload
	}
	if chunkSize <= 0 {
		return nil, fmt.Errorf("resumable pack chunk size must be > 0")
	}

	packHash := sha256Hex(compressed)
	chunks := (len(compressed) + chunkSize - 1) / chunkSize
	if chunks == 0 {
		chunks = 1
	}

	token := strings.TrimSpace(opts.RetryToken)
	result := &ResumablePackUploadResult{
		RetryToken: token,
		Chunks:     chunks,
		Bytes:      len(compressed),
	}
	for i, offset := 0, 0; i < chunks; i, offset = i+1, offset+chunkSize {
		end := offset + chunkSize
		if end > len(compressed) {
			end = len(compressed)
		}
		chunk := compressed[offset:end]
		resp, err := c.pushResumablePackChunk(ctx, chunk, resumablePackChunkRequest{
			Index:      i,
			Count:      chunks,
			Offset:     offset,
			PackHash:   packHash,
			ChunkHash:  sha256Hex(chunk),
			RetryToken: token,
		})
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(resp.UploadID) != "" {
			result.UploadID = strings.TrimSpace(resp.UploadID)
		}
		if strings.TrimSpace(resp.RetryToken) != "" {
			token = strings.TrimSpace(resp.RetryToken)
			result.RetryToken = token
		}
		if resp.Received > 0 && resp.Received != len(chunk) {
			return nil, fmt.Errorf("resumable pack chunk %d acknowledged %d bytes, sent %d", i, resp.Received, len(chunk))
		}
		if i == chunks-1 && !resp.Complete {
			return nil, fmt.Errorf("resumable pack upload incomplete after final chunk")
		}
	}
	return result, nil
}

type resumablePackChunkRequest struct {
	Index      int
	Count      int
	Offset     int
	PackHash   string
	ChunkHash  string
	RetryToken string
}

type resumablePackChunkResponse struct {
	UploadID   string `json:"upload_id,omitempty"`
	RetryToken string `json:"retry_token,omitempty"`
	Received   int    `json:"received,omitempty"`
	Complete   bool   `json:"complete,omitempty"`
}

func (c *Client) pushResumablePackChunk(ctx context.Context, chunk []byte, meta resumablePackChunkRequest) (resumablePackChunkResponse, error) {
	if err := c.checkPayloadLimit(len(chunk), "resumable pack chunk"); err != nil {
		return resumablePackChunkResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.BaseURL+"/objects/resumable", bytes.NewReader(chunk))
	if err != nil {
		return resumablePackChunkResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-graft-pack-chunk")
	req.Header.Set("Content-Encoding", "zstd")
	req.Header.Set("Graft-Chunk-Index", strconv.Itoa(meta.Index))
	req.Header.Set("Graft-Chunk-Count", strconv.Itoa(meta.Count))
	req.Header.Set("Graft-Chunk-Offset", strconv.Itoa(meta.Offset))
	req.Header.Set("Graft-Chunk-SHA256", meta.ChunkHash)
	req.Header.Set("Graft-Pack-SHA256", meta.PackHash)
	if strings.TrimSpace(meta.RetryToken) != "" {
		req.Header.Set("Graft-Retry-Token", strings.TrimSpace(meta.RetryToken))
	}

	body, err := c.doWithLimit(req, http.StatusOK, 1<<20, "application/json")
	if err != nil {
		return resumablePackChunkResponse{}, err
	}
	var resp resumablePackChunkResponse
	if len(strings.TrimSpace(string(body))) == 0 {
		return resp, nil
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return resumablePackChunkResponse{}, fmt.Errorf("decode resumable pack response: %w", err)
	}
	return resp, nil
}

func encodeCompressedPackForUpload(objects []ObjectRecord) ([]byte, []byte, error) {
	records := append([]ObjectRecord(nil), objects...)
	for i, obj := range records {
		if _, err := parseObjectType(string(obj.Type)); err != nil {
			return nil, nil, fmt.Errorf("push object %d: %w", i, err)
		}
		computedHash := object.HashObject(obj.Type, obj.Data)
		if provided := object.Hash(strings.TrimSpace(string(obj.Hash))); provided != "" && provided != computedHash {
			return nil, nil, fmt.Errorf("push object %d: hash mismatch (provided %s, computed %s)", i, provided, computedHash)
		}
		records[i].Hash = computedHash
	}

	packData, err := EncodePackTransportToBytes(records)
	if err != nil {
		return nil, nil, fmt.Errorf("encode pack: %w", err)
	}
	compressed, err := compressZstd(packData)
	if err != nil {
		return nil, nil, fmt.Errorf("compress pack: %w", err)
	}
	return packData, compressed, nil
}

// UpdateRefs applies atomic CAS updates on the remote refs.
func (c *Client) UpdateRefs(ctx context.Context, updates []RefUpdate) (map[string]object.Hash, error) {
	if len(updates) == 0 {
		return nil, fmt.Errorf("at least one ref update is required")
	}

	type refUpdatePayload struct {
		Name string  `json:"name"`
		Old  *string `json:"old,omitempty"`
		New  *string `json:"new"`
	}
	payload := struct {
		Updates []refUpdatePayload `json:"updates"`
	}{
		Updates: make([]refUpdatePayload, 0, len(updates)),
	}
	for _, u := range updates {
		name := strings.TrimSpace(u.Name)
		if name == "" {
			return nil, fmt.Errorf("ref update name is required")
		}
		var oldStr *string
		if u.Old != nil {
			v := strings.TrimSpace(string(*u.Old))
			if v != "" {
				if err := ValidateHash(object.Hash(v)); err != nil {
					return nil, fmt.Errorf("invalid old hash for ref %q: %w", name, err)
				}
			}
			oldStr = &v
		}
		var newStr *string
		if u.New != nil {
			v := strings.TrimSpace(string(*u.New))
			if v != "" {
				if err := ValidateHash(object.Hash(v)); err != nil {
					return nil, fmt.Errorf("invalid new hash for ref %q: %w", name, err)
				}
			}
			newStr = &v
		} else {
			empty := ""
			newStr = &empty
		}
		payload.Updates = append(payload.Updates, refUpdatePayload{
			Name: name,
			Old:  oldStr,
			New:  newStr,
		})
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.BaseURL+"/refs", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	body, err := c.doWithLimit(req, http.StatusOK, 1<<20, "application/json")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Updated map[string]string `json:"updated"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode ref update response: %w", err)
	}

	out := make(map[string]object.Hash, len(resp.Updated))
	for name, hash := range resp.Updated {
		out[name] = object.Hash(strings.TrimSpace(hash))
	}
	return out, nil
}

func (c *Client) doWithLimit(req *http.Request, expectedStatus int, maxBytes int64, expectedContentType string) ([]byte, error) {
	c.applyAuth(req)
	resp, err := retryDo(c.httpClient, req, c.maxAttempts)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	c.cacheServerMetadata(resp)

	body, readErr := readAllLimited(resp.Body, c.effectiveResponseLimit(maxBytes))
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode != expectedStatus {
		if re := tryParseRemoteError(body); re != nil {
			return nil, re
		}
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("remote request failed (%s %s): %s", req.Method, req.URL.Path, msg)
	}

	// Validate content type on success responses before returning body.
	if expectedContentType != "" {
		ct := resp.Header.Get("Content-Type")
		if ct != "" && !strings.HasPrefix(ct, expectedContentType) {
			return nil, fmt.Errorf("unexpected content type %q (expected %s) from %s %s (status %d)",
				ct, expectedContentType, req.Method, req.URL.Path, resp.StatusCode)
		}
	}

	return body, nil
}

// do is a backward-compatible wrapper using default limits.
func (c *Client) do(req *http.Request, expectedStatus int) ([]byte, error) {
	return c.doWithLimit(req, expectedStatus, responseLimitDefault, "")
}

func readAllLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes < 0 {
		return nil, fmt.Errorf("response byte limit must be >= 0 (got %d)", maxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%w: limit=%d", ErrRemoteResponseTooLarge, maxBytes)
	}
	return body, nil
}

func (c *Client) effectiveMaxBatchObjects(requested int) int {
	if requested <= 0 {
		if limits := c.ServerLimits(); limits != nil && limits.MaxBatch > 0 {
			return limits.MaxBatch
		}
		return requested
	}
	if limits := c.ServerLimits(); limits != nil && limits.MaxBatch > 0 && limits.MaxBatch < requested {
		return limits.MaxBatch
	}
	return requested
}

func (c *Client) effectiveResponseLimit(defaultLimit int64) int64 {
	if defaultLimit <= 0 {
		return defaultLimit
	}
	if limits := c.ServerLimits(); limits != nil && limits.MaxPayload > 0 && int64(limits.MaxPayload) < defaultLimit {
		return int64(limits.MaxPayload)
	}
	return defaultLimit
}

func (c *Client) effectiveObjectResponseLimit(defaultLimit int64) int64 {
	limit := c.effectiveResponseLimit(defaultLimit)
	if limits := c.ServerLimits(); limits != nil && limits.MaxObject > 0 && int64(limits.MaxObject) < limit {
		return int64(limits.MaxObject)
	}
	return limit
}

func (c *Client) checkPayloadLimit(size int, operation string) error {
	limits := c.ServerLimits()
	if limits == nil || limits.MaxPayload <= 0 || size <= limits.MaxPayload {
		return nil
	}
	return fmt.Errorf("%w: %s payload %d exceeds remote max_payload %d", ErrRemoteLimitExceeded, operation, size, limits.MaxPayload)
}

func (c *Client) checkObjectUploadLimits(objects []ObjectRecord) error {
	limits := c.ServerLimits()
	if limits == nil {
		return nil
	}
	if limits.MaxBatch > 0 && len(objects) > limits.MaxBatch {
		return fmt.Errorf("%w: object batch has %d objects, remote max_batch is %d", ErrRemoteLimitExceeded, len(objects), limits.MaxBatch)
	}
	if limits.MaxObject <= 0 {
		return nil
	}
	for _, obj := range objects {
		if len(obj.Data) > limits.MaxObject {
			hash := obj.Hash
			if hash == "" && obj.Type != "" {
				hash = object.HashObject(obj.Type, obj.Data)
			}
			return fmt.Errorf("%w: object %s is %d bytes, remote max_object is %d", ErrRemoteLimitExceeded, shortRemoteHash(hash), len(obj.Data), limits.MaxObject)
		}
	}
	return nil
}

func (c *Client) limitHaveHashesForPayload(haves []object.Hash) []object.Hash {
	limits := c.ServerLimits()
	if limits == nil || limits.MaxPayload <= 0 {
		return haves
	}
	max := maxHaveHashesForPayload(limits.MaxPayload)
	if max <= 0 {
		return nil
	}
	if len(haves) <= max {
		return haves
	}
	return haves[len(haves)-max:]
}

func maxHaveHashesForPayload(maxPayload int) int {
	const (
		batchPayloadFixedOverhead = 512
		batchHashBudgetBytes      = 80
	)
	remaining := maxPayload - batchPayloadFixedOverhead
	if remaining <= 0 {
		return 0
	}
	return remaining / batchHashBudgetBytes
}

func shortRemoteHash(h object.Hash) string {
	s := strings.TrimSpace(string(h))
	if len(s) > 12 {
		return s[:12]
	}
	if s == "" {
		return "<unknown>"
	}
	return s
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validateHashList(hashes []object.Hash, label string) ([]object.Hash, error) {
	out := make([]object.Hash, 0, len(hashes))
	for _, h := range hashes {
		s := strings.TrimSpace(string(h))
		if s == "" {
			continue
		}
		hash := object.Hash(s)
		if err := ValidateHash(hash); err != nil {
			return nil, fmt.Errorf("invalid %s hash %q: %w", label, s, err)
		}
		out = append(out, hash)
	}
	return out, nil
}

func hashStrings(hashes []object.Hash) []string {
	out := make([]string, 0, len(hashes))
	for _, h := range hashes {
		out = append(out, string(h))
	}
	return out
}

func (c *Client) applyAuth(req *http.Request) {
	req.Header.Set(headerProtocol, ProtocolVersion)
	req.Header.Set(headerCapabilities, ClientCapabilities)

	if strings.TrimSpace(c.token) != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
		return
	}
	if strings.TrimSpace(c.user) != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
}

func parseObjectType(raw string) (object.ObjectType, error) {
	switch object.ObjectType(strings.TrimSpace(raw)) {
	case object.TypeBlob, object.TypeTag, object.TypeTree, object.TypeCommit, object.TypeEntity, object.TypeEntityList:
		return object.ObjectType(strings.TrimSpace(raw)), nil
	default:
		return "", fmt.Errorf("unsupported object type %q", raw)
	}
}

// IsPackUploadUnsupported reports whether err indicates the remote rejected
// pack upload transport and the caller should fall back to compatibility mode.
func IsPackUploadUnsupported(err error) bool {
	return errors.Is(err, ErrPackUploadUnsupported)
}

func isPackUploadUnsupportedResponse(status int, msg string) bool {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnsupportedMediaType, http.StatusNotImplemented:
		return true
	}
	msg = strings.ToLower(strings.TrimSpace(msg))
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "unsupported media type") || strings.Contains(msg, "not implemented") {
		return true
	}
	if strings.Contains(msg, "application/x-graft-pack") || strings.Contains(msg, "content-encoding") || strings.Contains(msg, "zstd") {
		return true
	}
	return strings.Contains(msg, "unsupported") && strings.Contains(msg, "pack")
}
