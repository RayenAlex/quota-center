package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeNativeAuthStore struct {
	entries []StoredCredential
}

func (s fakeNativeAuthStore) List(context.Context) ([]StoredCredential, error) {
	return s.entries, nil
}

func (s fakeNativeAuthStore) Get(_ context.Context, authIndex string) (StoredCredential, error) {
	for _, entry := range s.entries {
		if entry.AuthIndex == authIndex {
			return entry, nil
		}
	}
	return StoredCredential{}, fmt.Errorf("not found")
}

func (s fakeNativeAuthStore) Save(context.Context, string, []byte) (StoredCredential, error) {
	return StoredCredential{}, fmt.Errorf("not used")
}

func TestAccountFromCPANativeCredentials(t *testing.T) {
	tests := []struct {
		name       string
		entry      StoredCredential
		wantLabel  string
		wantToken  string
		wantPlan   string
		wantSource AuthSource
	}{
		{
			name: "codex",
			entry: StoredCredential{
				AuthIndex: "codex-index",
				Name:      "codex-account.json",
				Provider:  ProviderCodex,
				Label:     "user@example.test",
				JSON:      json.RawMessage(`{"type":"codex","email":"user@example.test","access_token":"codex-access","account_id":"acct-1"}`),
			},
			wantLabel:  "user@example.test",
			wantToken:  "codex-access",
			wantPlan:   "oauth",
			wantSource: AuthSourceCPA,
		},
		{
			name: "gemini nested token",
			entry: StoredCredential{
				AuthIndex: "gemini-index",
				Name:      "geminicli.json",
				Provider:  ProviderGemini,
				Label:     "user@example.test",
				JSON:      json.RawMessage(`{"type":"gemini-cli","email":"user@example.test","token":{"access_token":"gemini-access","expiresAt":9999999999999}}`),
			},
			wantLabel:  "user@example.test",
			wantToken:  "gemini-access",
			wantPlan:   "oauth",
			wantSource: AuthSourceCPA,
		},
		{
			name: "antigravity gemini",
			entry: StoredCredential{
				AuthIndex: "antigravity-index",
				Name:      "antigravity-user.json",
				Provider:  "antigravity",
				Label:     "gemini@example.test",
				JSON:      json.RawMessage(`{"type":"antigravity","email":"gemini@example.test","access_token":"antigravity-access","project_id":"proj-1"}`),
			},
			wantLabel:  "gemini@example.test",
			wantToken:  "antigravity-access",
			wantPlan:   "",
			wantSource: AuthSourceCPA,
		},
		{
			name: "xai grok",
			entry: StoredCredential{
				AuthIndex: "xai-index",
				Name:      "xai-account.json",
				Provider:  ProviderGrok,
				Label:     "grok@example.test",
				JSON:      json.RawMessage(`{"type":"xai","email":"grok@example.test","access_token":"grok-access"}`),
			},
			wantLabel:  "grok@example.test",
			wantToken:  "grok-access",
			wantPlan:   "oauth",
			wantSource: AuthSourceCPA,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := accountFromStoredCredential(tt.entry)
			if err != nil {
				t.Fatalf("accountFromStoredCredential() error = %v", err)
			}
			if plan.ID != tt.entry.AuthIndex || plan.Label != tt.wantLabel || plan.AccessToken != tt.wantToken || plan.Plan != tt.wantPlan || plan.Source != tt.wantSource {
				t.Fatalf("plan = %#v", plan)
			}
			if plan.APIKey != "" {
				t.Fatal("native OAuth credential should not be copied into APIKey")
			}
			if tt.name == "antigravity gemini" && (plan.Provider != ProviderGemini || plan.AccountID != "proj-1") {
				t.Fatalf("antigravity plan = %#v", plan)
			}
		})
	}
}

func TestHostAuthStoreMapsNativeProviderAliases(t *testing.T) {
	got := hostProviderAliases("gemini-cli", "")
	if got != ProviderGemini {
		t.Fatalf("hostProviderAliases(gemini-cli) = %q", got)
	}
	got = hostProviderAliases("antigravity", "")
	if got != ProviderGemini {
		t.Fatalf("hostProviderAliases(antigravity) = %q", got)
	}
	got = hostProviderAliases("", "xai")
	if got != ProviderGrok {
		t.Fatalf("hostProviderAliases(xai) = %q", got)
	}
	if hostProviderAliases("unknown", "") != "unknown" {
		t.Fatal("unknown provider should be preserved for later diagnostics")
	}
}

func TestCPANativeAccountsResolveAndRefresh(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{}
	client := NewClient(QuotaHTTPClientFunc(func(ctx context.Context, req *http.Request) (*http.Response, error) {
		mu.Lock()
		seen["path"] = req.URL.Path
		seen["auth"] = req.Header.Get("Authorization")
		mu.Unlock()
		switch req.URL.Host {
		case "chatgpt.com":
			return jsonResponse(http.StatusOK, `{"rate_limit":{"primary_window":{"used_percent":20,"limit_window_seconds":18000}}}`), nil
		case "cloudcode-pa.googleapis.com":
			if strings.HasSuffix(req.URL.Path, "loadCodeAssist") {
				return jsonResponse(http.StatusOK, `{"cloudaicompanionProject":{"id":"project-1"}}`), nil
			}
			return jsonResponse(http.StatusOK, `{"buckets":[{"modelId":"gemini-2.5-pro","remainingFraction":0.75}]}`), nil
		case "grok.com":
			return jsonResponse(http.StatusOK, `invalid-grpc-for-now`), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`), nil
		}
	}))
	dispatcher := NewDispatcher(client)
	dispatcher.auth = fakeNativeAuthStore{entries: []StoredCredential{
		{AuthIndex: "codex-index", Name: "codex.json", Provider: ProviderCodex, Label: "Codex Account", JSON: json.RawMessage(`{"type":"codex","access_token":"secret-codex-token","account_id":"acct-1"}`)},
		{AuthIndex: "gemini-index", Name: "geminicli.json", Provider: ProviderGemini, Label: "Gemini Account", JSON: json.RawMessage(`{"type":"gemini","token":{"access_token":"secret-gemini-token"}}`)},
		{AuthIndex: "grok-index", Name: "xai.json", Provider: ProviderGrok, Label: "Grok Account", JSON: json.RawMessage(`{"type":"xai","access_token":"secret-grok-token"}`)},
		{AuthIndex: "unknown-index", Name: "unknown.json", Provider: "unknown"},
	}}
	dispatcher.config.Timeout = time.Second
	dispatcher.service = NewService(dispatcher.config, client)

	plans := dispatcher.resolvePlans(context.Background(), dispatcher.config.Plans, dispatcher.auth)
	if len(plans) != 3 {
		t.Fatalf("len(plans) = %d, want 3; plans = %#v", len(plans), plans)
	}
	results := dispatcher.refreshResults(context.Background(), plans, "")
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	if results[0].Provider != ProviderCodex || results[0].Quota == nil || len(results[0].Quota.Windows) != 1 || results[0].Error != "" {
		t.Fatalf("codex result = %#v", results[0])
	}
	if results[1].Provider != ProviderGemini || results[1].Quota == nil || len(results[1].Quota.Windows) != 1 || results[1].Error != "" {
		t.Fatalf("gemini result = %#v", results[1])
	}
	if results[2].Provider != ProviderGrok || results[2].Quota != nil || results[2].Error == "" || !strings.Contains(results[2].Error, "experimental") {
		t.Fatalf("grok result = %#v", results[2])
	}
	raw := string(RenderStatusJSON(results))
	for _, secret := range []string{"secret-codex-token", "secret-gemini-token", "secret-grok-token"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("status JSON leaked %q", secret)
		}
	}
	_ = seen
}

func TestNativeCredentialStatusesCreateClearResults(t *testing.T) {
	dispatcher := NewTestRPC()
	dispatcher.auth = fakeNativeAuthStore{entries: []StoredCredential{
		{AuthIndex: "missing-token", Name: "codex.json", Provider: ProviderCodex, JSON: json.RawMessage(`{"type":"codex"}`)},
		{AuthIndex: "disabled", Name: "gemini.json", Provider: ProviderGemini, Disabled: true},
	}}
	dispatcher.config.Timeout = time.Second
	dispatcher.service = NewService(dispatcher.config, dispatcher.client)

	results := dispatcher.refreshResults(context.Background(), dispatcher.resolvePlans(context.Background(), dispatcher.config.Plans, dispatcher.auth), "")
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2; results = %#v", len(results), results)
	}
	if !strings.Contains(results[0].Error, "缺少可用 Token") {
		t.Fatalf("missing token result = %#v", results[0])
	}
	if !strings.Contains(results[1].Error, "已禁用") {
		t.Fatalf("disabled result = %#v", results[1])
	}
}
