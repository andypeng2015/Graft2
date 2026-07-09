// Package remote implements the graft remote protocol for synchronizing
// repositories, including pack transport, object fetching, ref advertisement,
// and push/pull operations.
package remote

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

const (
	// ProtocolVersion is the current Graft protocol version.
	ProtocolVersion = "1"

	// ClientCapabilities lists all capabilities this client supports.
	ClientCapabilities = "pack,zstd,sideband,resumable-pack"

	headerProtocol     = "Graft-Protocol"
	headerCapabilities = "Graft-Capabilities"
	headerLimits       = "Graft-Limits"
)

// Well-known capability names used in the Graft protocol.
const (
	CapPack          = "pack"
	CapZstd          = "zstd"
	CapSideband      = "sideband"
	CapShallow       = "shallow"
	CapFilter        = "filter"
	CapIncludeTag    = "include-tag"
	CapResumablePack = "resumable-pack"
)

// ValidateHash checks that a hash is a valid 64-character lowercase hex string (SHA-256).
func ValidateHash(h object.Hash) error {
	s := strings.TrimSpace(string(h))
	if s == "" {
		return fmt.Errorf("hash is empty")
	}
	if len(s) != 64 {
		return fmt.Errorf("hash length %d, expected 64", len(s))
	}
	if _, err := hex.DecodeString(s); err != nil {
		return fmt.Errorf("hash contains non-hex characters: %w", err)
	}
	return nil
}

// Capabilities represents a set of protocol capabilities.
type Capabilities struct {
	set map[string]struct{}
}

// ParseCapabilities parses a comma-separated capability string.
func ParseCapabilities(raw string) Capabilities {
	caps := Capabilities{set: make(map[string]struct{})}
	for _, cap := range strings.Split(raw, ",") {
		cap = strings.TrimSpace(cap)
		if cap != "" {
			caps.set[cap] = struct{}{}
		}
	}
	return caps
}

// Has returns true if the capability is present.
func (c Capabilities) Has(name string) bool {
	_, ok := c.set[name]
	return ok
}

// Intersect returns capabilities present in both sets.
func (c Capabilities) Intersect(other Capabilities) Capabilities {
	result := Capabilities{set: make(map[string]struct{})}
	for k := range c.set {
		if _, ok := other.set[k]; ok {
			result.set[k] = struct{}{}
		}
	}
	return result
}

// Len returns the number of capabilities in the set.
func (c Capabilities) Len() int { return len(c.set) }

// Add inserts a capability into the set.
func (c *Capabilities) Add(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if c.set == nil {
		c.set = make(map[string]struct{})
	}
	c.set[name] = struct{}{}
}

// Names returns the capability names in sorted order.
func (c Capabilities) Names() []string {
	names := make([]string, 0, len(c.set))
	for k := range c.set {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// String returns a sorted comma-separated capability string.
func (c Capabilities) String() string {
	return strings.Join(c.Names(), ",")
}

// ProtocolContract describes the current public Graft remote protocol in a
// machine-readable form for CLI introspection and conformance tests.
type ProtocolContract struct {
	ProtocolVersion     string                  `json:"protocolVersion"`
	Documentation       string                  `json:"documentation"`
	BaseURLFormat       string                  `json:"baseUrlFormat"`
	DefaultOrchardHost  string                  `json:"defaultOrchardHost"`
	HashFunction        string                  `json:"hashFunction"`
	Headers             []ProtocolHeader        `json:"headers"`
	ClientCapabilities  []string                `json:"clientCapabilities"`
	DefinedCapabilities []ProtocolCapability    `json:"definedCapabilities"`
	Transports          []ProtocolTransport     `json:"transports"`
	ServerLimits        []ProtocolLimit         `json:"serverLimits"`
	ResponseLimits      []ProtocolResponseLimit `json:"responseLimits"`
	Endpoints           []ProtocolEndpoint      `json:"endpoints"`
	ObjectTypes         []string                `json:"objectTypes"`
	ErrorShape          ProtocolErrorShape      `json:"errorShape"`
}

type ProtocolHeader struct {
	Name        string `json:"name"`
	Direction   string `json:"direction"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type ProtocolCapability struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ProtocolTransport struct {
	Name                string `json:"name"`
	RequestContentType  string `json:"requestContentType,omitempty"`
	ResponseContentType string `json:"responseContentType,omitempty"`
	ContentEncoding     string `json:"contentEncoding,omitempty"`
	Description         string `json:"description"`
}

type ProtocolLimit struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ProtocolResponseLimit struct {
	Name        string `json:"name"`
	Bytes       int64  `json:"bytes"`
	Description string `json:"description"`
}

type ProtocolEndpoint struct {
	Name                 string   `json:"name"`
	Scope                string   `json:"scope"`
	Method               string   `json:"method"`
	Path                 string   `json:"path"`
	RequestContentTypes  []string `json:"requestContentTypes,omitempty"`
	ResponseContentTypes []string `json:"responseContentTypes,omitempty"`
	Description          string   `json:"description"`
}

type ProtocolErrorShape struct {
	ContentType          string `json:"contentType"`
	CodeField            string `json:"codeField"`
	MessageField         string `json:"messageField"`
	DetailField          string `json:"detailField"`
	RetryableStatusCodes []int  `json:"retryableStatusCodes"`
}

// SupportedProtocolContract returns the protocol surface supported by this
// client build. The values intentionally mirror the normative protocol spec and
// the constants enforced by the client implementation.
func SupportedProtocolContract() ProtocolContract {
	return ProtocolContract{
		ProtocolVersion:    ProtocolVersion,
		Documentation:      "docs/protocol-spec.md",
		BaseURLFormat:      "https://{host}/graft/{owner}/{repo}",
		DefaultOrchardHost: "https://orchard.dev",
		HashFunction:       "sha256",
		Headers: []ProtocolHeader{
			{
				Name:        headerProtocol,
				Direction:   "request",
				Required:    true,
				Description: "protocol version; current value is " + ProtocolVersion,
			},
			{
				Name:        headerCapabilities,
				Direction:   "request,response",
				Required:    false,
				Description: "comma-separated capability names advertised by the client and optionally by the server",
			},
			{
				Name:        headerLimits,
				Direction:   "response",
				Required:    false,
				Description: "comma-separated server limit key/value pairs",
			},
			{
				Name:        "X-Object-Type",
				Direction:   "response",
				Required:    true,
				Description: "object type for GET {base}/objects/{hash} responses",
			},
			{
				Name:        "X-Truncated",
				Direction:   "response",
				Required:    false,
				Description: "true when a pack batch response has more objects available",
			},
			{
				Name:        "X-Shallow",
				Direction:   "response",
				Required:    false,
				Description: "comma-separated shallow boundary hashes for shallow fetch responses",
			},
		},
		ClientCapabilities: ParseCapabilities(ClientCapabilities).Names(),
		DefinedCapabilities: []ProtocolCapability{
			{Name: CapPack, Description: "Git-compatible pack binary transport"},
			{Name: CapZstd, Description: "zstd compression for pack payloads"},
			{Name: CapSideband, Description: "length-prefixed multiplexed sideband streams"},
			{Name: CapShallow, Description: "shallow clone boundaries"},
			{Name: CapFilter, Description: "partial clone object filters"},
			{Name: CapIncludeTag, Description: "include tag objects when fetching tagged commits"},
			{Name: CapResumablePack, Description: "chunked pack uploads with SHA-256 chunk hashes and retry tokens"},
		},
		Transports: []ProtocolTransport{
			{
				Name:                "json",
				RequestContentType:  "application/json",
				ResponseContentType: "application/json",
				Description:         "control messages and JSON object batch fallback",
			},
			{
				Name:                "ndjson",
				RequestContentType:  "application/x-ndjson",
				ResponseContentType: "application/json",
				Description:         "newline-delimited JSON object upload compatibility mode",
			},
			{
				Name:                "pack",
				RequestContentType:  "application/x-graft-pack",
				ResponseContentType: "application/x-graft-pack",
				ContentEncoding:     "zstd",
				Description:         "binary object batch and upload transport",
			},
		},
		ServerLimits: []ProtocolLimit{
			{Name: "max_batch", Type: "integer", Description: "maximum objects in a single batch response or upload"},
			{Name: "max_payload", Type: "integer", Description: "maximum request or response payload size in bytes"},
			{Name: "max_object", Type: "integer", Description: "maximum single object size in bytes"},
		},
		ResponseLimits: []ProtocolResponseLimit{
			{Name: "defaultControl", Bytes: responseLimitDefault, Description: "default control response read cap"},
			{Name: "refs", Bytes: responseLimitRefs, Description: "GET {base}/refs response read cap"},
			{Name: "batchObjects", Bytes: responseLimitBatch, Description: "POST {base}/objects/batch response read cap"},
			{Name: "singleObject", Bytes: responseLimitObject, Description: "GET {base}/objects/{hash} response read cap"},
			{Name: "ack", Bytes: 1 << 20, Description: "ref update and object upload acknowledgement read cap"},
		},
		Endpoints: []ProtocolEndpoint{
			{
				Name:                 "listRefs",
				Scope:                "repository",
				Method:               "GET",
				Path:                 "{base}/refs",
				ResponseContentTypes: []string{"application/json"},
				Description:          "list repository refs with paginated and legacy flat-map response support",
			},
			{
				Name:                 "updateRefs",
				Scope:                "repository",
				Method:               "POST",
				Path:                 "{base}/refs",
				RequestContentTypes:  []string{"application/json"},
				ResponseContentTypes: []string{"application/json"},
				Description:          "atomically update refs with compare-and-swap semantics",
			},
			{
				Name:                 "batchObjects",
				Scope:                "repository",
				Method:               "POST",
				Path:                 "{base}/objects/batch",
				RequestContentTypes:  []string{"application/json"},
				ResponseContentTypes: []string{"application/json", "application/x-graft-pack"},
				Description:          "fetch objects reachable from wants and not reachable from haves",
			},
			{
				Name:                 "getObject",
				Scope:                "repository",
				Method:               "GET",
				Path:                 "{base}/objects/{hash}",
				ResponseContentTypes: []string{"application/octet-stream"},
				Description:          "fetch one raw object by SHA-256 hash",
			},
			{
				Name:                 "pushObjects",
				Scope:                "repository",
				Method:               "POST",
				Path:                 "{base}/objects",
				RequestContentTypes:  []string{"application/x-ndjson", "application/x-graft-pack"},
				ResponseContentTypes: []string{"application/json"},
				Description:          "upload object records using NDJSON compatibility mode or zstd pack mode",
			},
			{
				Name:                 "pushObjectsResumable",
				Scope:                "repository",
				Method:               "POST",
				Path:                 "{base}/objects/resumable",
				RequestContentTypes:  []string{"application/x-graft-pack-chunk"},
				ResponseContentTypes: []string{"application/json"},
				Description:          "upload zstd pack chunks with chunk hashes and retry token continuation",
			},
			{
				Name:                 "createRepository",
				Scope:                "management",
				Method:               "POST",
				Path:                 "{host}/api/v1/repos",
				RequestContentTypes:  []string{"application/json"},
				ResponseContentTypes: []string{"application/json"},
				Description:          "create an Orchard repository",
			},
			{
				Name:                 "requestMagicLink",
				Scope:                "management",
				Method:               "POST",
				Path:                 "{host}/api/v1/auth/magic/request",
				RequestContentTypes:  []string{"application/json"},
				ResponseContentTypes: []string{"application/json"},
				Description:          "request a magic-link authentication token",
			},
			{
				Name:                 "verifyMagicToken",
				Scope:                "management",
				Method:               "POST",
				Path:                 "{host}/api/v1/auth/magic/verify",
				RequestContentTypes:  []string{"application/json"},
				ResponseContentTypes: []string{"application/json"},
				Description:          "exchange a magic token for a bearer token",
			},
			{
				Name:                 "beginSSHChallenge",
				Scope:                "management",
				Method:               "POST",
				Path:                 "{host}/api/v1/auth/ssh/challenge",
				RequestContentTypes:  []string{"application/json"},
				ResponseContentTypes: []string{"application/json"},
				Description:          "begin SSH challenge/response authentication",
			},
			{
				Name:                 "verifySSHChallenge",
				Scope:                "management",
				Method:               "POST",
				Path:                 "{host}/api/v1/auth/ssh/verify",
				RequestContentTypes:  []string{"application/json"},
				ResponseContentTypes: []string{"application/json"},
				Description:          "verify an SSH challenge signature and return a bearer token",
			},
			{
				Name:                 "mintSSHBootstrapToken",
				Scope:                "management",
				Method:               "POST",
				Path:                 "{host}/api/v1/auth/ssh/bootstrap/token",
				RequestContentTypes:  []string{"application/json"},
				ResponseContentTypes: []string{"application/json"},
				Description:          "mint a short-lived SSH bootstrap token",
			},
			{
				Name:                 "bootstrapSSHRegistration",
				Scope:                "management",
				Method:               "POST",
				Path:                 "{host}/api/v1/auth/ssh/bootstrap",
				RequestContentTypes:  []string{"application/json"},
				ResponseContentTypes: []string{"application/json"},
				Description:          "register an SSH public key with a bootstrap token",
			},
			{
				Name:                 "registerSSHKey",
				Scope:                "management",
				Method:               "POST",
				Path:                 "{host}/api/v1/user/ssh-keys",
				RequestContentTypes:  []string{"application/json"},
				ResponseContentTypes: []string{"application/json"},
				Description:          "register an SSH public key for an authenticated user",
			},
		},
		ObjectTypes: []string{
			string(object.TypeBlob),
			string(object.TypeCommit),
			string(object.TypeEntity),
			string(object.TypeEntityList),
			string(object.TypeTag),
			string(object.TypeTree),
		},
		ErrorShape: ProtocolErrorShape{
			ContentType:          "application/json",
			CodeField:            "code",
			MessageField:         "error",
			DetailField:          "detail",
			RetryableStatusCodes: []int{429, 500, 502, 503, 504},
		},
	}
}

// RemoteError is a structured error from the remote server.
type RemoteError struct {
	Code    string `json:"code"`
	Message string `json:"error"`
	Detail  string `json:"detail,omitempty"`
}

func (e *RemoteError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s (%s): %s", e.Message, e.Code, e.Detail)
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

// ServerLimits holds server-advertised protocol limits parsed from the Graft-Limits header.
type ServerLimits struct {
	MaxBatch   int // max objects per batch (0 = use client default)
	MaxPayload int // max payload bytes (0 = use client default)
	MaxObject  int // max single object bytes (0 = use client default)
}

// ParseLimits parses a Graft-Limits header value.
// Format: "max_batch=50000,max_payload=67108864,max_object=33554432"
// Unknown keys are ignored. Invalid values are ignored (field stays 0).
func ParseLimits(raw string) ServerLimits {
	var limits ServerLimits
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 {
			continue
		}
		switch key {
		case "max_batch":
			limits.MaxBatch = n
		case "max_payload":
			limits.MaxPayload = n
		case "max_object":
			limits.MaxObject = n
		}
	}
	return limits
}

// tryParseRemoteError attempts to parse a JSON error response body.
func tryParseRemoteError(body []byte) *RemoteError {
	var re RemoteError
	if err := json.Unmarshal(body, &re); err != nil {
		return nil
	}
	if re.Message == "" && re.Code == "" {
		return nil
	}
	return &re
}
