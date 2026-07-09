package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/remote"
)

func TestProtocolCmdJSON(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"protocol", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONProtocolOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if result.ProtocolVersion != remote.ProtocolVersion {
		t.Fatalf("protocolVersion = %q, want %q", result.ProtocolVersion, remote.ProtocolVersion)
	}
	if !stringSliceContains(result.ClientCapabilities, remote.CapPack) {
		t.Fatalf("clientCapabilities missing %q: %#v", remote.CapPack, result.ClientCapabilities)
	}
	if !protocolEndpointExists(result.Endpoints, "GET", "{base}/refs") {
		t.Fatalf("protocol endpoints missing GET {base}/refs: %#v", result.Endpoints)
	}
	if !protocolEndpointExists(result.Endpoints, "POST", "{base}/objects/batch") {
		t.Fatalf("protocol endpoints missing POST {base}/objects/batch: %#v", result.Endpoints)
	}
	if result.ErrorShape.CodeField != "code" || result.ErrorShape.MessageField != "error" {
		t.Fatalf("unexpected error shape: %#v", result.ErrorShape)
	}
}

func TestProtocolCmdText(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"protocol"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw := out.String()
	for _, want := range []string{
		"Graft protocol " + remote.ProtocolVersion,
		"Graft-Protocol",
		"GET  {base}/refs",
		"POST {base}/objects/batch",
		"max_payload",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("protocol text missing %q\nraw:\n%s", want, raw)
		}
	}
}

func protocolEndpointExists(endpoints []remote.ProtocolEndpoint, method, path string) bool {
	for _, endpoint := range endpoints {
		if endpoint.Method == method && endpoint.Path == path {
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
