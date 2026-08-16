package main

import (
	"testing"
	"time"
)

func TestDecodeConfigDefaults(t *testing.T) {
	cfg, err := DecodeConfig(nil, func(string) string { return "" })
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if cfg.Priority != 1 {
		t.Fatalf("Priority = %d, want 1", cfg.Priority)
	}
	if cfg.Endpoint != "https://bigmodel.cn/api/monitor/usage/quota/limit" {
		t.Fatalf("Endpoint = %q", cfg.Endpoint)
	}
	if cfg.Timeout != 15*time.Second {
		t.Fatalf("Timeout = %s, want 15s", cfg.Timeout)
	}
	if len(cfg.Plans) != 0 {
		t.Fatalf("Plans = %#v, want empty", cfg.Plans)
	}
}

func TestDecodeConfigPlans(t *testing.T) {
	raw := []byte(`
enabled: true
priority: 20
timeout: 30s
endpoint: https://bigmodel.cn/api/monitor/usage/quota/limit
plans:
  - id: glm-pro
    label: GLM Pro
    api_key_env: ZHIPU_PRO_KEY
  - id: glm-max
    label: "GLM Max"
    api_key_env: ZHIPU_MAX_KEY
`)
	cfg, err := DecodeConfig(raw, func(name string) string {
		if name != "ZHIPU_PRO_KEY" && name != "ZHIPU_MAX_KEY" {
			t.Fatalf("getenv(%q) was called", name)
		}
		return name + "-value"
	})
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}
	if len(cfg.Plans) != 2 {
		t.Fatalf("len(Plans) = %d, want 2", len(cfg.Plans))
	}
	if cfg.Plans[1].ID != "glm-max" || cfg.Plans[1].Label != "GLM Max" || cfg.Plans[1].APIKey != "ZHIPU_MAX_KEY-value" {
		t.Fatalf("Plans[1] = %#v", cfg.Plans[1])
	}
	if cfg.Plans[0].APIKeyEnv != "ZHIPU_PRO_KEY" {
		t.Fatalf("APIKeyEnv = %q", cfg.Plans[0].APIKeyEnv)
	}
}

func TestDecodeConfigRejectsInvalidPlans(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{name: "missing id", yaml: "plans:\n- label: x\n  api_key_env: K\n", want: "plans[0].id is required"},
		{name: "invalid id", yaml: "plans:\n- id: bad id\n  label: x\n  api_key_env: K\n", want: "plans[0].id"},
		{name: "missing env", yaml: "plans:\n- id: x\n  label: x\n", want: "plans[0].api_key_env is required"},
		{name: "missing env value", yaml: "plans:\n- id: x\n  label: x\n  api_key_env: MISSING\n", want: `environment variable "MISSING" is empty`},
		{name: "duplicate id", yaml: "plans:\n- id: x\n  label: x\n  api_key_env: K1\n- id: x\n  label: y\n  api_key_env: K2\n", want: `duplicate plan id "x"`},
		{name: "bad timeout", yaml: "timeout: nope\n", want: "timeout"},
		{name: "bad endpoint", yaml: "endpoint: ftp://example.test\n", want: "endpoint"},
		{name: "global endpoint host", yaml: "endpoint: https://attacker.test/limit\n", want: "endpoint"},
		{name: "zhipu endpoint host", yaml: "plans:\n- id: x\n  api_key_env: K\n  endpoint: https://attacker.test/limit\n", want: "plans[0].endpoint"},
		{name: "minimax endpoint host", yaml: "accounts:\n- id: x\n  provider: minimax\n  api_key_env: K\n  endpoint: https://attacker.test/limit\n", want: "accounts[0].endpoint"},
		{name: "fixed endpoint provider", yaml: "accounts:\n- id: x\n  provider: codex\n  api_key_env: K\n  endpoint: https://chatgpt.com/usage\n", want: "accounts[0].endpoint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeConfig([]byte(tt.yaml), func(name string) string {
				if name == "K" || name == "K1" || name == "K2" {
					return "key"
				}
				return ""
			})
			if err == nil || !contains(err.Error(), tt.want) {
				t.Fatalf("DecodeConfig() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || index(s, substr) >= 0)
}

func index(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
