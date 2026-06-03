package cli

import (
	"strings"
	"testing"
)

func TestResolveEachItems(t *testing.T) {
	// Comma list passes through untouched.
	got, err := resolveEachItems("a.ts,b.ts")
	if err != nil {
		t.Fatalf("resolveEachItems: %v", err)
	}
	if len(got) != 2 || got[0] != "a.ts" || got[1] != "b.ts" {
		t.Errorf("got %v", got)
	}
}

func TestResolveEachItemsRejectsEmpty(t *testing.T) {
	if _, err := resolveEachItems(""); err == nil {
		t.Fatal("expected error for empty --each")
	}
}

func TestFilterByOnly(t *testing.T) {
	ids := []string{"a", "b", "c"}
	got := filterIDs(ids, "a,c")
	if strings.Join(got, ",") != "a,c" {
		t.Errorf("got %v", got)
	}
	if all := filterIDs(ids, ""); len(all) != 3 {
		t.Errorf("empty --only should keep all, got %v", all)
	}
}
