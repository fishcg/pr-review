package lib

import (
	"strings"
	"testing"
)

func hasEnv(entries []string, key, val string) bool {
	target := key + "=" + val
	for _, e := range entries {
		if e == target {
			return true
		}
	}
	return false
}

func countEnvKey(entries []string, key string) int {
	prefix := key + "="
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e, prefix) {
			count++
		}
	}
	return count
}

func TestFilterAndSetCodexEnv_KeepExistingWhenConfigEmpty(t *testing.T) {
	in := []string{
		"OPENAI_API_KEY=from_env",
		"OPENAI_BASE_URL=https://env.example.com",
		"OPENAI_MODEL=env-model",
		"OTHER_VAR=1",
	}

	out := filterAndSetCodexEnv(in, "", "", "")

	if !hasEnv(out, "OPENAI_API_KEY", "from_env") {
		t.Fatalf("expected OPENAI_API_KEY from env to be preserved")
	}
	if !hasEnv(out, "OPENAI_BASE_URL", "https://env.example.com") {
		t.Fatalf("expected OPENAI_BASE_URL from env to be preserved")
	}
	if !hasEnv(out, "OPENAI_MODEL", "env-model") {
		t.Fatalf("expected OPENAI_MODEL from env to be preserved")
	}
	if !hasEnv(out, "OTHER_VAR", "1") {
		t.Fatalf("expected OTHER_VAR to be preserved")
	}
}

func TestFilterAndSetCodexEnv_OverrideConfiguredValues(t *testing.T) {
	in := []string{
		"OPENAI_API_KEY=from_env",
		"OPENAI_BASE_URL=https://env.example.com",
		"OPENAI_MODEL=env-model",
		"OTHER_VAR=1",
	}

	out := filterAndSetCodexEnv(in, "from_config", "", "config-model")

	if !hasEnv(out, "OPENAI_API_KEY", "from_config") {
		t.Fatalf("expected OPENAI_API_KEY to be overridden by config")
	}
	if !hasEnv(out, "OPENAI_BASE_URL", "https://env.example.com") {
		t.Fatalf("expected OPENAI_BASE_URL to keep env value when config empty")
	}
	if !hasEnv(out, "OPENAI_MODEL", "config-model") {
		t.Fatalf("expected OPENAI_MODEL to be overridden by config")
	}
	if countEnvKey(out, "OPENAI_API_KEY") != 1 || countEnvKey(out, "OPENAI_MODEL") != 1 {
		t.Fatalf("expected overridden keys to appear exactly once")
	}
}
