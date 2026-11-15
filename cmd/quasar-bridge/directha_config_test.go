package main

import (
	"os"
	"strings"
	"testing"
)

func TestLocalModeDoesNotRequireLLM(t *testing.T) {
	for _, key := range []string{"QUASAR_MODE", "QUASAR_LLM_BASE_URL", "QUASAR_LLM_MODEL", "QUASAR_LLM_API", "QUASAR_HOME_MCP_URL"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	raw := `{"mode":"local","airplay":{"disabled":true},"assistant_tools":{"enabled":false},"routing":{"rules":[],"default_route":"yandex","classifier":{"enabled":false},"refusal":{"enabled":false}},"direct_ha":{"url":"http://192.168.1.11:8123","rules":[{"phrases":["включи свет"],"domain":"switch","service":"turn_on","entity_id":"switch.cabinet"}]}}`
	cfg, err := parseConfig([]byte(raw), false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLMBaseURL != "" || cfg.LLMModel != "" || cfg.Mode != "local" {
		t.Fatal("unexpected LLM requirement")
	}
	for _, bad := range []string{strings.Replace(raw, `"default_route":"yandex"`, `"default_route":"llm"`, 1), strings.Replace(raw, `"classifier":{"enabled":false}`, `"classifier":{"enabled":true}`, 1), strings.Replace(raw, `"refusal":{"enabled":false}`, `"refusal":{"enabled":true}`, 1)} {
		if _, err := parseConfig([]byte(bad), false); err == nil {
			t.Error("unsafe local configuration accepted")
		}
	}
}
