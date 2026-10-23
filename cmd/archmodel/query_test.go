package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func runQ(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	full := append([]string{"query"}, args...)
	full = append(full, "--root", sinkRoot, "--config", exampleYA)
	var out, errb bytes.Buffer
	code := run(full, &out, &errb)
	var v map[string]any
	if out.Len() > 0 {
		if err := json.Unmarshal(out.Bytes(), &v); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, out.String())
		}
	}
	return code, v
}

func TestQueryUnknownNameSuggests(t *testing.T) {
	code, v := runQ(t, "module", "auht")
	if code != 1 || !strings.Contains(v["error"].(string), "auht") {
		t.Fatalf("code=%d %v", code, v)
	}
	if d := v["did_you_mean"].([]any); len(d) == 0 || d[0] != "auth" {
		t.Errorf("did_you_mean = %v", d)
	}
	if code, v := runQ(t, "operation", "logn"); code != 1 || v["did_you_mean"].([]any)[0] != "login" {
		t.Errorf("%d %v", code, v)
	}
}

func TestQueryFlagsMayFollowArguments(t *testing.T) {
	// runQ already puts --root after the positional; this must not be a usage error.
	if code, _ := runQ(t, "who-depends", "config"); code != 0 {
		t.Errorf("code=%d", code)
	}
}

func TestQueryUsageErrors(t *testing.T) {
	var out, errb bytes.Buffer
	for _, args := range [][]string{{"query"}, {"query", "bogus"}, {"query", "module"}, {"query", "rules", "x"}} {
		if code := run(args, &out, &errb); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}

func TestQueryModuleCard(t *testing.T) {
	_, v := runQ(t, "module", "events")
	owns := v["owns"].(map[string]any)
	if tables := owns["tables"].([]any); len(tables) != 1 || tables[0] != "outbox" {
		t.Errorf("events should own outbox: %v", owns["tables"])
	}
	if m := v["metrics"].(map[string]any); m["reading"] == "" {
		t.Error("metrics need a reading")
	}
	_, v = runQ(t, "module", "testsupport")
	if _, has := v["metrics"]; has || v["kind"] != "test_only" {
		t.Errorf("test-only card: %v", v)
	}
}

func TestQueryOperationClosesOverSchemas(t *testing.T) {
	_, v := runQ(t, "operation", "login")
	ids := map[string]bool{}
	for _, s := range v["schemas"].([]any) {
		ids[s.(map[string]any)["id"].(string)] = true
	}
	for _, want := range []string{"auth.LoginRequest", "auth.AuthResponse", "auth.User"} {
		if !ids[want] {
			t.Errorf("missing schema %s in %v", want, ids)
		}
	}
	if v["implementation"] == nil || v["rate_limit"] == nil {
		t.Errorf("login: %v", v)
	}
}

func TestQueryListsAndRules(t *testing.T) {
	_, v := runQ(t, "modules")
	if len(v["modules"].([]any)) != 15 {
		t.Logf("modules: %d", len(v["modules"].([]any)))
	}
	_, v = runQ(t, "operations")
	if len(v["operations"].([]any)) == 0 {
		t.Error("no operations")
	}
	_, v = runQ(t, "rules")
	if len(v["rules"].([]any)) != 5 || len(v["metric_rationales"].(map[string]any)) < 7 {
		t.Errorf("rules: %v", v)
	}
}
