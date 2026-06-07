package coord

import "testing"

// OtherAgentPresence returns the read-presence of agents other than the active
// one, in a deterministic order, so `coord check` can surface who else is
// currently looking at the same code.
func TestOtherAgentPresence(t *testing.T) {
	entries := []PresenceEntry{
		{AgentID: "me", AgentName: "me", File: "a.go"},
		{AgentID: "birch", AgentName: "birch", File: "b.go", Entity: "decl:function_definition::Foo:0"},
		{AgentID: "cedar", AgentName: "cedar", File: "a.go"},
	}

	got := OtherAgentPresence(entries, "me")

	if len(got) != 2 {
		t.Fatalf("expected 2 other-agent entries, got %d", len(got))
	}
	// Sorted by File, then Entity, then AgentName: a.go/cedar before b.go/birch.
	if got[0].AgentID != "cedar" || got[1].AgentID != "birch" {
		t.Fatalf("unexpected order: %s then %s", got[0].AgentID, got[1].AgentID)
	}
}

// With no active agent, every reader is "other".
func TestOtherAgentPresence_NoActiveAgent(t *testing.T) {
	entries := []PresenceEntry{
		{AgentID: "birch", File: "b.go"},
		{AgentID: "cedar", File: "a.go"},
	}
	if got := OtherAgentPresence(entries, ""); len(got) != 2 {
		t.Fatalf("expected all 2 entries with no active agent, got %d", len(got))
	}
}
