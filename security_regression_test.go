package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestUnauthenticatedResourceDoesNotReadCredentialsOrCallUpstream(t *testing.T) {
	var requests int
	client := NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(http.StatusServiceUnavailable, `{}`), nil
	}))
	dispatcher := NewDispatcher(client)
	dispatcher.auth = fakeNativeAuthStore{entries: []StoredCredential{{
		AuthIndex: "codex-index",
		Provider:  ProviderCodex,
		Name:      "codex.json",
		JSON:      json.RawMessage(`{"type":"codex","access_token":"secret-codex-token"}`),
	}}}
	dispatcher.service = NewService(dispatcher.config, client)
	dispatcher.lastResults = []PlanResult{{
		ID:       "cached-native-account",
		Provider: ProviderCodex,
		Label:    "cached@example.test",
		Source:   AuthSourceCPA,
		Quota:    &QuotaResult{Windows: []QuotaWindow{{Name: "five_hour", RemainingPercent: 75}}},
	}}

	response := dispatcher.handleManagement(pluginManagementRequest(http.MethodGet, resourceStatusPath))
	body := string(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, body)
	}
	if requests != 0 {
		t.Fatalf("resource triggered %d upstream requests", requests)
	}
	for _, leaked := range []string{"codex-index", "codex.json", "secret-codex-token", "cached-native-account", "cached@example.test", "75.0%", "CPA 认证"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("resource leaked %q", leaked)
		}
	}
}

func TestResourceDoesNotExposeAddAccountEndpointOrWriteCredential(t *testing.T) {
	store := &fakeSavingAuthStore{}
	dispatcher := NewTestRPC()
	dispatcher.auth = store
	dispatcher.accounts = store

	response := dispatcher.handleManagement(pluginManagementRequest(
		http.MethodPost,
		"/v0/resource/plugins/quota-center/status",
		[]byte(`{"provider":"zhipu","label":"unauthenticated","credential":"secret-key"}`),
	))

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	if store.saved != nil {
		t.Fatal("unauthenticated resource write persisted a credential")
	}
	if strings.Contains(string(response.Body), "secret-key") {
		t.Fatal("unauthenticated resource response leaked credential")
	}
}

func TestAddAccountStoresCredentialUnderProviderSemanticField(t *testing.T) {
	type savedCredential struct {
		Provider    string `json:"provider"`
		APIKey      string `json:"api_key,omitempty"`
		AccessToken string `json:"access_token,omitempty"`
	}
	tests := []struct {
		provider Provider
		want     savedCredential
	}{
		{ProviderZhipu, savedCredential{Provider: ProviderZhipu, APIKey: "secret-key"}},
		{ProviderMiniMax, savedCredential{Provider: ProviderMiniMax, APIKey: "secret-key"}},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			store := &fakeSavingAuthStore{}
			dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, `{"data":{"limits":[]}}`), nil
			})))
			dispatcher.auth = store
			dispatcher.accounts = store
			response := dispatcher.handleManagement(pluginManagementRequest(http.MethodPost, managementAccountsPath, []byte(fmt.Sprintf(
				`{"provider":%q,"label":"work","credential":"secret-key"}`, tt.provider,
			))))
			if response.StatusCode != http.StatusCreated {
				t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
			}
			var got savedCredential
			if err := json.Unmarshal(store.saved, &got); err != nil {
				t.Fatalf("decode saved credential: %v", err)
			}
			if got != tt.want {
				t.Fatalf("saved credential = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestAddMiniMaxAccountPersistsValidatedEndpoint(t *testing.T) {
	var seenHost, seenPath, seenAuth string
	store := &fakeSavingAuthStore{}
	dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(_ context.Context, req *http.Request) (*http.Response, error) {
		seenHost, seenPath, seenAuth = req.URL.Host, req.URL.Path, req.Header.Get("Authorization")
		return jsonResponse(http.StatusOK, `{"model_remains":[{"model_name":"general","current_interval_remaining_percent":80}],"base_resp":{"status_code":0}}`), nil
	})))
	dispatcher.auth = store
	dispatcher.accounts = store
	response := dispatcher.handleManagement(pluginManagementRequest(http.MethodPost, managementAccountsPath, []byte(
		`{"provider":"minimax","plan":"coding-plan","label":"MiniMax 国际站","credential":"secret-key","endpoint":"`+miniMaxGlobalEndpoint+`"}`,
	)))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	if seenHost != "api.minimax.io" || seenPath != "/v1/api/openplatform/coding_plan/remains" || seenAuth != "Bearer secret-key" {
		t.Fatalf("verification request = %s %s %q", seenHost, seenPath, seenAuth)
	}
	var saved struct {
		Provider string `json:"provider"`
		Plan     string `json:"plan"`
		APIKey   string `json:"api_key"`
		Endpoint string `json:"endpoint"`
	}
	if err := json.Unmarshal(store.saved, &saved); err != nil {
		t.Fatalf("decode saved credential: %v", err)
	}
	if saved.Provider != string(ProviderMiniMax) || saved.Plan != "coding-plan" || saved.APIKey != "secret-key" || saved.Endpoint != miniMaxGlobalEndpoint {
		t.Fatalf("saved credential = %#v", saved)
	}
	if strings.Contains(string(response.Body), "secret-key") {
		t.Fatal("management response leaked credential")
	}
}

func TestAddMiniMaxAccountRejectsUntrustedEndpoint(t *testing.T) {
	var requests int
	store := &fakeSavingAuthStore{}
	dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
		requests++
		t.Fatal("untrusted endpoint must not call upstream")
		return nil, nil
	})))
	dispatcher.auth = store
	dispatcher.accounts = store
	response := dispatcher.handleManagement(pluginManagementRequest(http.MethodPost, managementAccountsPath, []byte(
		`{"provider":"minimax","plan":"coding-plan","label":"work","credential":"secret-key","endpoint":"https://attacker.test/steal"}`,
	)))
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	if requests != 0 || store.saved != nil {
		t.Fatalf("untrusted endpoint side effects: requests=%d saved=%v", requests, store.saved != nil)
	}
	if strings.Contains(string(response.Body), "secret-key") {
		t.Fatal("endpoint validation response leaked credential")
	}
}

func TestAddAccountRejectsCPANativeProviders(t *testing.T) {
	for _, provider := range []Provider{ProviderCodex, ProviderGemini, ProviderGrok} {
		store := &fakeSavingAuthStore{}
		dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
			t.Fatal("native provider add must not call upstream")
			return jsonResponse(http.StatusOK, `{}`), nil
		})))
		dispatcher.auth = store
		dispatcher.accounts = store
		response := dispatcher.handleManagement(pluginManagementRequest(http.MethodPost, managementAccountsPath, []byte(fmt.Sprintf(
			`{"provider":%q,"label":"work","credential":"secret-key"}`, provider,
		))))
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s status = %d, body = %s", provider, response.StatusCode, response.Body)
		}
		if store.saved != nil {
			t.Fatalf("%s must not persist a CPA auth file", provider)
		}
	}
}

func TestMaskCredentialNeverRevealsPrefixOrSuffix(t *testing.T) {
	if got := maskCredential("abcdefghijklmnop"); got != "••••••••" {
		t.Fatalf("maskCredential() = %q", got)
	}
}

func TestClientRejectsEndpointRedirect(t *testing.T) {
	var seenAuth string
	client := NewClient(QuotaHTTPClientFunc(func(ctx context.Context, req *http.Request) (*http.Response, error) {
		if req.URL.Host == "attacker.test" {
			seenAuth = req.Header.Get("Authorization")
		}
		return jsonResponse(http.StatusFound, `{}`), nil
	}))
	client.SetEndpoint("https://bigmodel.cn/api/monitor/usage/quota/limit")

	_, err := client.FetchQuota(context.Background(), PlanConfig{
		ID:       "redirect",
		Provider: ProviderZhipu,
		APIKey:   "secret-key",
		Endpoint: "https://bigmodel.cn/api/monitor/usage/quota/limit",
	})

	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("FetchQuota() error = %v, want redirect rejection", err)
	}
	if seenAuth != "" {
		t.Fatalf("redirect target received Authorization %q", seenAuth)
	}
}

func TestClientRejectsMiniMaxRedirectBeforeCallingTarget(t *testing.T) {
	var targetCalled bool
	client := NewClient(QuotaHTTPClientFunc(func(ctx context.Context, req *http.Request) (*http.Response, error) {
		if req.URL.Host == "attacker.test" {
			targetCalled = true
		}
		return jsonResponse(http.StatusFound, `{}`), nil
	}))

	_, err := client.FetchQuota(context.Background(), PlanConfig{
		ID:       "minimax-redirect",
		Provider: ProviderMiniMax,
		APIKey:   "secret-key",
	})

	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("FetchQuota() error = %v, want redirect rejection", err)
	}
	if targetCalled {
		t.Fatal("redirect target was called")
	}
}

func TestAddAccountRejectsUnsafeLabel(t *testing.T) {
	dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"data":{"limits":[]}}`), nil
	})))
	store := &fakeSavingAuthStore{}
	dispatcher.auth = store
	dispatcher.accounts = store
	response := dispatcher.handleManagement(pluginManagementRequest(http.MethodPost, managementAccountsPath, []byte(
		`{"provider":"zhipu","label":"`+strings.Repeat("x", 129)+`","credential":"secret-key"}`,
	)))
	if response.StatusCode != http.StatusBadRequest || strings.Contains(string(response.Body), "credential") {
		t.Fatalf("response = %d %s", response.StatusCode, response.Body)
	}
	if store.saved != nil {
		t.Fatal("unsafe label must not persist a credential")
	}
}

func TestConcurrentConfigureAndManagementCallsUseConfigurationSnapshots(t *testing.T) {
	dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"data":{"limits":[]}}`), nil
	})))

	var wait sync.WaitGroup
	for index := 0; index < 50; index++ {
		wait.Add(2)
		go func(index int) {
			defer wait.Done()
			config := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("endpoint: https://bigmodel.cn/api/monitor/usage/quota/limit?iteration=%d", index)))
			request := fmt.Sprintf(`{"config_yaml":%q}`, config)
			if _, err := dispatcher.Call("plugin.reconfigure", []byte(request)); err != nil {
				t.Errorf("reconfigure: %v", err)
			}
		}(index)
		go func() {
			defer wait.Done()
			response := dispatcher.handleManagement(pluginManagementRequest(http.MethodGet, managementStatusPath))
			if response.StatusCode != http.StatusOK {
				t.Errorf("status response = %d %s", response.StatusCode, response.Body)
			}
		}()
	}
	wait.Wait()
}

func TestAddAccountRejectsOversizedRequestBody(t *testing.T) {
	dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"data":{"limits":[]}}`), nil
	})))
	store := &fakeSavingAuthStore{}
	dispatcher.auth = store
	dispatcher.accounts = store
	body := []byte(`{"provider":"zhipu","label":"work","credential":"` + strings.Repeat("x", maxAddAccountBodyBytes) + `"}`)
	response := dispatcher.handleManagement(pluginManagementRequest(http.MethodPost, managementAccountsPath, body))
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("response = %d %s", response.StatusCode, response.Body)
	}
	if store.saved != nil {
		t.Fatal("oversized request must not persist a credential")
	}
}

func TestRefreshUsesCacheOnlyWhenPlanSequenceMatches(t *testing.T) {
	dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"data":{"limits":[]}}`), nil
	})))
	dispatcher.service = NewService(dispatcher.config, dispatcher.client)
	dispatcher.lastResults = []PlanResult{{ID: "old-a", Provider: ProviderZhipu, Label: "Old A"}, {ID: "old-b", Provider: ProviderZhipu, Label: "Old B"}}
	dispatcher.lastPlanIDs = []string{"old-a", "old-b"}

	results := dispatcher.refreshResults(context.Background(), []PlanConfig{
		{ID: "new-a", Provider: ProviderZhipu, Label: "New A"},
		{ID: "new-b", Provider: ProviderZhipu, Label: "New B"},
	}, "new-a")
	if results[0].ID != "new-a" || results[0].Label != "New A" {
		t.Fatalf("results = %#v", results)
	}
}

func TestUpdateAccountKeepsExistingIDAndOverwritesCredential(t *testing.T) {
	store := &fakeSavingAuthStore{}
	dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"data":{"limits":[]}}`), nil
	})))
	dispatcher.auth = store
	dispatcher.accounts = store
	response := dispatcher.handleManagement(pluginManagementRequest(http.MethodPost, managementAccountsPath, []byte(
		`{"id":"zhipu-work","provider":"zhipu","label":"新名称","credential":"new-secret-key"}`,
	)))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	if store.name != "zhipu-work.json" {
		t.Fatalf("saved name = %q", store.name)
	}
	var got map[string]string
	if err := json.Unmarshal(store.saved, &got); err != nil {
		t.Fatalf("decode saved credential: %v", err)
	}
	if got["id"] != "zhipu-work" || got["label"] != "新名称" || got["api_key"] != "new-secret-key" {
		t.Fatalf("saved credential = %#v", got)
	}
}

func TestUpdateAccountRejectsCPANative(t *testing.T) {
	dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"data":{"limits":[]}}`), nil
	})))
	dispatcher.auth = fakeNativeAuthStore{entries: []StoredCredential{{
		AuthIndex: "codex-index",
		Name:      "codex.json",
		Provider:  ProviderCodex,
		Label:     "Codex",
		JSON:      json.RawMessage(`{"type":"codex","access_token":"secret-codex-token","account_id":"acct-1"}`),
	}}}
	response := dispatcher.handleManagement(pluginManagementRequest(http.MethodPost, managementAccountsPath, []byte(
		`{"id":"codex-index","provider":"codex","label":"Codex","credential":"new-token"}`,
	)))
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
}

func TestDeleteAccountRemovesSnapshotAndPluginAccount(t *testing.T) {
	store := &fileAccountStore{dir: t.TempDir()}
	credential, _ := json.Marshal(map[string]any{
		"id":       "zhipu-work",
		"provider": ProviderZhipu,
		"type":     ProviderZhipu,
		"label":    "工作账号",
		"api_key":  "secret-key",
	})
	if _, err := store.Save(context.Background(), "zhipu-work.json", credential); err != nil {
		t.Fatal(err)
	}
	dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"data":{"limits":[]}}`), nil
	})))
	dispatcher.accounts = store
	dispatcher.lastResults = []PlanResult{{ID: "zhipu-work", Provider: ProviderZhipu, Label: "工作账号"}}
	req := pluginManagementRequest(http.MethodDelete, managementAccountsPath)
	req.Query = map[string][]string{"id": {"zhipu-work"}}
	response := dispatcher.handleManagement(req)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	if entries, err := store.List(context.Background()); err != nil || len(entries) != 0 {
		t.Fatalf("remaining accounts = %#v err=%v", entries, err)
	}
	resource := dispatcher.handleManagement(pluginManagementRequest(http.MethodGet, resourceStatusPath))
	if strings.Contains(string(resource.Body), `data-account-id="zhipu-work"`) {
		t.Fatal("deleted account remained in resource snapshot")
	}
}

func TestDeleteAccountRejectsCPANative(t *testing.T) {
	dispatcher := NewDispatcher(NewTestRPC().client)
	dispatcher.auth = fakeNativeAuthStore{entries: []StoredCredential{{
		AuthIndex: "codex-index",
		Name:      "codex.json",
		Provider:  ProviderCodex,
		Label:     "Codex",
		JSON:      json.RawMessage(`{"type":"codex","access_token":"secret-codex-token","account_id":"acct-1"}`),
	}}}
	req := pluginManagementRequest(http.MethodDelete, managementAccountsPath)
	req.Query = map[string][]string{"id": {"codex-index"}}
	response := dispatcher.handleManagement(req)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
}

func TestResourceDoesNotDeleteAccount(t *testing.T) {
	var store *fileAccountStore
	dispatcher := NewTestRPC()
	dispatcher.auth = store
	dispatcher.accounts = store
	req := pluginManagementRequest(http.MethodDelete, resourceStatusPath)
	req.Query = map[string][]string{"id": {"zhipu-work"}}
	response := dispatcher.handleManagement(req)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	if store != nil {
		t.Fatal("resource delete touched account store")
	}
}

func TestAddAccountDoesNotExposeResourceSnapshot(t *testing.T) {
	store := &fakeSavingAuthStore{}
	dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"Result":{"QuotaUsage":[{"Level":"weekly","Percent":10}]}}`), nil
	})))
	dispatcher.auth = store
	dispatcher.accounts = store
	dispatcher.lastResults = []PlanResult{{ID: "legacy-result-private", Provider: ProviderZhipu, Label: "旧账号私有数据"}}
	response := dispatcher.handleManagement(pluginManagementRequest(http.MethodPost, managementAccountsPath, []byte(
		`{"provider":"ark","label":"工作方舟-已保存","access_id":"AKLT-example","secret":"secret-ark"}`,
	)))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	resource := dispatcher.handleManagement(pluginManagementRequest(http.MethodGet, resourceStatusPath))
	body := string(resource.Body)
	for _, leaked := range []string{"工作方舟-已保存", "旧账号私有数据", "legacy-result-private", "secret-ark", "AKLT-example"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("public resource leaked account snapshot %q", leaked)
		}
	}
}

func TestAddAccountStoresArkAccessKeys(t *testing.T) {
	store := &fakeSavingAuthStore{}
	dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"Result":{"QuotaUsage":[{"Level":"weekly","Percent":10}]}}`), nil
	})))
	dispatcher.auth = store
	dispatcher.accounts = store
	response := dispatcher.handleManagement(pluginManagementRequest(http.MethodPost, managementAccountsPath, []byte(
		`{"provider":"ark","label":"方舟主账号","access_id":"AKLT-example","secret":"secret-ark"}`,
	)))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	var got struct {
		Provider        string `json:"provider"`
		AccessKeyID     string `json:"access_key_id"`
		SecretAccessKey string `json:"secret_access_key"`
		APIKey          string `json:"api_key"`
	}
	if err := json.Unmarshal(store.saved, &got); err != nil {
		t.Fatalf("decode saved credential: %v", err)
	}
	if got.Provider != string(ProviderArk) || got.AccessKeyID != "AKLT-example" || got.SecretAccessKey != "secret-ark" || got.APIKey != "" {
		t.Fatalf("saved credential = %#v", got)
	}
}

type fakeSavingAuthStore struct {
	saved []byte
	name  string
}

func (s *fakeSavingAuthStore) List(context.Context) ([]StoredCredential, error) {
	return nil, nil
}

func (s *fakeSavingAuthStore) Get(context.Context, string) (StoredCredential, error) {
	return StoredCredential{}, fmt.Errorf("not found")
}

func (s *fakeSavingAuthStore) Save(_ context.Context, name string, credential []byte) (StoredCredential, error) {
	s.name = name
	s.saved = append([]byte(nil), credential...)
	return StoredCredential{Name: name}, nil
}

func pluginManagementRequest(method, path string, body ...[]byte) pluginapi.ManagementRequest {
	return pluginapi.ManagementRequest{
		Method: method,
		Path:   path,
		Body:   firstOptional(body),
	}
}

func firstOptional(values [][]byte) []byte {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

func binaryResponse(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
}
