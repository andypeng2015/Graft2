package remote

import (
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestValidateHashValid(t *testing.T) {
	valid := object.Hash("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	if err := ValidateHash(valid); err != nil {
		t.Fatalf("valid hash rejected: %v", err)
	}
}

func TestValidateHashEmpty(t *testing.T) {
	if err := ValidateHash(""); err == nil {
		t.Fatal("empty hash accepted")
	}
}

func TestValidateHashWrongLength(t *testing.T) {
	if err := ValidateHash("abc123"); err == nil {
		t.Fatal("short hash accepted")
	}
}

func TestValidateHashNonHex(t *testing.T) {
	bad := object.Hash("g1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	if err := ValidateHash(bad); err == nil {
		t.Fatal("non-hex hash accepted")
	}
}

func TestValidateHashWhitespace(t *testing.T) {
	if err := ValidateHash("  "); err == nil {
		t.Fatal("whitespace-only hash accepted")
	}
}

func TestParseCapabilities(t *testing.T) {
	caps := ParseCapabilities("pack,zstd,sideband")
	if !caps.Has("pack") {
		t.Fatal("missing pack capability")
	}
	if !caps.Has("zstd") {
		t.Fatal("missing zstd capability")
	}
	if !caps.Has("sideband") {
		t.Fatal("missing sideband capability")
	}
	if caps.Has("nonexistent") {
		t.Fatal("unexpected capability")
	}
}

func TestCapabilitiesIntersect(t *testing.T) {
	a := ParseCapabilities("pack,zstd,sideband")
	b := ParseCapabilities("pack,zstd")
	common := a.Intersect(b)
	if !common.Has("pack") || !common.Has("zstd") {
		t.Fatal("missing intersected capability")
	}
	if common.Has("sideband") {
		t.Fatal("sideband should not be in intersection")
	}
}

func TestCapabilitiesString(t *testing.T) {
	caps := ParseCapabilities("zstd,pack,sideband")
	s := caps.String()
	if s != "pack,sideband,zstd" {
		t.Fatalf("String() = %q, want %q", s, "pack,sideband,zstd")
	}
}

func TestCapabilitiesAddZeroValue(t *testing.T) {
	var caps Capabilities
	caps.Add(" pack ")
	caps.Add("")
	if !caps.Has("pack") {
		t.Fatal("zero-value Add did not record capability")
	}
	if caps.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", caps.Len())
	}
}

func TestCapabilitiesNamesSorted(t *testing.T) {
	caps := ParseCapabilities("zstd,pack,sideband")
	got := caps.Names()
	want := []string{"pack", "sideband", "zstd"}
	if len(got) != len(want) {
		t.Fatalf("Names() len = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names()[%d] = %q, want %q (all: %#v)", i, got[i], want[i], got)
		}
	}
}

func TestParseLimitsComplete(t *testing.T) {
	limits := ParseLimits("max_batch=50000,max_payload=67108864,max_object=33554432")
	if limits.MaxBatch != 50000 {
		t.Fatalf("MaxBatch = %d, want 50000", limits.MaxBatch)
	}
	if limits.MaxPayload != 67108864 {
		t.Fatalf("MaxPayload = %d, want 67108864", limits.MaxPayload)
	}
	if limits.MaxObject != 33554432 {
		t.Fatalf("MaxObject = %d, want 33554432", limits.MaxObject)
	}
}

func TestParseLimitsEmpty(t *testing.T) {
	limits := ParseLimits("")
	if limits.MaxBatch != 0 || limits.MaxPayload != 0 || limits.MaxObject != 0 {
		t.Fatalf("expected zero limits for empty string, got %+v", limits)
	}
}

func TestParseLimitsPartial(t *testing.T) {
	limits := ParseLimits("max_batch=1000")
	if limits.MaxBatch != 1000 {
		t.Fatalf("MaxBatch = %d, want 1000", limits.MaxBatch)
	}
	if limits.MaxPayload != 0 {
		t.Fatalf("MaxPayload should be 0 for missing key, got %d", limits.MaxPayload)
	}
}

func TestParseLimitsInvalidValue(t *testing.T) {
	limits := ParseLimits("max_batch=abc,max_payload=100")
	if limits.MaxBatch != 0 {
		t.Fatalf("MaxBatch should be 0 for invalid value, got %d", limits.MaxBatch)
	}
	if limits.MaxPayload != 100 {
		t.Fatalf("MaxPayload = %d, want 100", limits.MaxPayload)
	}
}

func TestParseLimitsNegativeIgnored(t *testing.T) {
	limits := ParseLimits("max_batch=-1")
	if limits.MaxBatch != 0 {
		t.Fatalf("MaxBatch should be 0 for negative value, got %d", limits.MaxBatch)
	}
}

func TestRemoteErrorFormat(t *testing.T) {
	re := &RemoteError{Code: "ref_not_found", Message: "ref not found", Detail: "heads/main"}
	if re.Error() != "ref not found (ref_not_found): heads/main" {
		t.Fatalf("Error() = %q", re.Error())
	}
}

func TestSupportedProtocolContractMatchesClientConstants(t *testing.T) {
	contract := SupportedProtocolContract()
	if contract.ProtocolVersion != ProtocolVersion {
		t.Fatalf("ProtocolVersion = %q, want %q", contract.ProtocolVersion, ProtocolVersion)
	}
	if got := strings.Join(contract.ClientCapabilities, ","); got != ParseCapabilities(ClientCapabilities).String() {
		t.Fatalf("ClientCapabilities = %q, want %q", got, ParseCapabilities(ClientCapabilities).String())
	}
	if !protocolHeaderExists(contract.Headers, headerProtocol, "request", true) {
		t.Fatalf("contract headers missing required request %s: %#v", headerProtocol, contract.Headers)
	}
	if !protocolEndpointExists(contract.Endpoints, "GET", "{base}/refs") {
		t.Fatalf("contract endpoints missing GET refs: %#v", contract.Endpoints)
	}
	if !protocolEndpointExists(contract.Endpoints, "POST", "{base}/objects/batch") {
		t.Fatalf("contract endpoints missing batch objects: %#v", contract.Endpoints)
	}
	if !protocolResponseLimitExists(contract.ResponseLimits, "refs", responseLimitRefs) {
		t.Fatalf("contract response limits missing refs=%d: %#v", responseLimitRefs, contract.ResponseLimits)
	}
	if !protocolResponseLimitExists(contract.ResponseLimits, "batchObjects", responseLimitBatch) {
		t.Fatalf("contract response limits missing batchObjects=%d: %#v", responseLimitBatch, contract.ResponseLimits)
	}
	if !stringSliceContains(contract.ObjectTypes, string(object.TypeEntityList)) {
		t.Fatalf("contract object types missing %q: %#v", object.TypeEntityList, contract.ObjectTypes)
	}
	if contract.ErrorShape.CodeField != "code" || contract.ErrorShape.MessageField != "error" || contract.ErrorShape.DetailField != "detail" {
		t.Fatalf("unexpected error shape: %#v", contract.ErrorShape)
	}
}

func protocolHeaderExists(headers []ProtocolHeader, name, direction string, required bool) bool {
	for _, header := range headers {
		if header.Name == name && strings.Contains(header.Direction, direction) && header.Required == required {
			return true
		}
	}
	return false
}

func protocolEndpointExists(endpoints []ProtocolEndpoint, method, path string) bool {
	for _, endpoint := range endpoints {
		if endpoint.Method == method && endpoint.Path == path {
			return true
		}
	}
	return false
}

func protocolResponseLimitExists(limits []ProtocolResponseLimit, name string, bytes int64) bool {
	for _, limit := range limits {
		if limit.Name == name && limit.Bytes == bytes {
			return true
		}
	}
	return false
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
