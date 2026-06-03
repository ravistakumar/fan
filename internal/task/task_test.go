package task

import "testing"

func anyAgent(string) bool { return true }

func TestParseResolvesDefaults(t *testing.T) {
	in := []byte(`
[defaults]
agent = "codex"
gate  = "npm test"
concurrency = 4

[[task]]
id = "a"
prompt = "do a"

[[task]]
id = "b"
prompt = "do b"
agent = "claude"
`)
	f, err := Parse(in, anyAgent)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Defaults.Concurrency != 4 {
		t.Errorf("concurrency = %d, want 4", f.Defaults.Concurrency)
	}
	if len(f.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(f.Tasks))
	}
	if f.Tasks[0].Agent != "codex" || f.Tasks[0].Gate != "npm test" {
		t.Errorf("task a not resolved from defaults: %+v", f.Tasks[0])
	}
	if f.Tasks[1].Agent != "claude" {
		t.Errorf("task b override = %q, want claude", f.Tasks[1].Agent)
	}
}

func TestParseRejectsDuplicateIDs(t *testing.T) {
	in := []byte(`
[[task]]
id = "x"
prompt = "one"
[[task]]
id = "x"
prompt = "two"
`)
	if _, err := Parse(in, anyAgent); err == nil {
		t.Fatal("expected duplicate-id error, got nil")
	}
}

func TestParseRejectsUnknownAgent(t *testing.T) {
	in := []byte(`
[[task]]
id = "x"
prompt = "one"
agent = "bogus"
`)
	reject := func(string) bool { return false }
	if _, err := Parse(in, reject); err == nil {
		t.Fatal("expected unknown-agent error, got nil")
	}
}

func TestParseRejectsEmptyPrompt(t *testing.T) {
	in := []byte(`
[[task]]
id = "x"
prompt = ""
`)
	if _, err := Parse(in, anyAgent); err == nil {
		t.Fatal("expected empty-prompt error, got nil")
	}
}
