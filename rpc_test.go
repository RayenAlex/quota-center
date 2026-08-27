package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestRPCRegistrationContract(t *testing.T) {
	dispatcher := NewTestRPC()
	raw, err := dispatcher.Call("plugin.register", []byte(`{"config_yaml":"dGltZW91dDogOXMK"}`))
	if err != nil {
		t.Fatalf("plugin.register error = %v", err)
	}
	var envelope struct {
		OK     bool `json:"ok"`
		Result struct {
			SchemaVersion uint32 `json:"schema_version"`
			Metadata      struct {
				Name    string `json:"Name"`
				Version string `json:"Version"`
			} `json:"metadata"`
			Capabilities struct {
				ManagementAPI bool `json:"management_api"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !envelope.OK || envelope.Result.SchemaVersion != 1 {
		t.Fatalf("envelope = %#v", envelope)
	}
	if envelope.Result.Metadata.Name != "额度中心" || envelope.Result.Metadata.Version != "0.2.0" {
		t.Fatalf("metadata = %#v", envelope.Result.Metadata)
	}
	if !envelope.Result.Capabilities.ManagementAPI {
		t.Fatalf("capabilities = %#v", envelope.Result.Capabilities)
	}
}

func TestRPCManagementContract(t *testing.T) {
	dispatcher := NewTestRPC()
	raw, err := dispatcher.Call("management.register", nil)
	if err != nil {
		t.Fatalf("management.register error = %v", err)
	}
	var registration struct {
		Resources []struct {
			Path        string
			Menu        string
			Description string
		}
	}
	if err := decodeRPCResult(raw, &registration); err != nil {
		t.Fatalf("decode registration: %v", err)
	}
	if len(registration.Resources) != 1 || registration.Resources[0].Path != "/status" || registration.Resources[0].Menu != "额度中心" {
		t.Fatalf("resources = %#v", registration.Resources)
	}

	raw, err = dispatcher.Call("management.handle", []byte(`{"Method":"GET","Path":"/plugins/quota-center/status"}`))
	if err != nil {
		t.Fatalf("management.handle error = %v", err)
	}
	var response struct {
		StatusCode int
		Headers    map[string][]string
		Body       []byte
	}
	if err := decodeRPCResult(raw, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.StatusCode != 200 || response.Headers["Content-Type"][0] != "text/html; charset=utf-8" || !contains(string(response.Body), "额度中心") {
		t.Fatalf("response = %#v", response)
	}
}

func TestRPCResourcePathRendersPanel(t *testing.T) {
	dispatcher := NewTestRPC()
	if _, err := dispatcher.Call("plugin.register", nil); err != nil {
		t.Fatalf("plugin.register error = %v", err)
	}

	raw, err := dispatcher.Call("management.handle", []byte(`{"Method":"GET","Path":"/v0/resource/plugins/quota-center/status"}`))
	if err != nil {
		t.Fatalf("management.handle error = %v", err)
	}
	var response struct {
		StatusCode int
		Body       []byte
	}
	if err := decodeRPCResult(raw, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	body := string(response.Body)
	if response.StatusCode != 200 || !contains(body, "额度中心") {
		t.Fatalf("response = %#v", response)
	}
	for _, want := range []string{
		`data-management-bootstrap="true"`,
		`const hasCPAAuth=stores.some(store=>`,
		`if(hasCPAAuth)document.querySelector('[data-management-view="overview"]')?.click()`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("public resource missing automatic authenticated bootstrap %q", want)
		}
	}
}

func TestRPCResourcePathDoesNotRenderCachedManagementResults(t *testing.T) {
	dispatcher := NewTestRPC()
	if _, err := dispatcher.Call("plugin.register", nil); err != nil {
		t.Fatalf("plugin.register error = %v", err)
	}
	dispatcher.auth = fakeNativeAuthStore{entries: []StoredCredential{{
		AuthIndex: "codex-index",
		Provider:  ProviderCodex,
		Name:      "codex.json",
		JSON:      json.RawMessage(`{"type":"codex","access_token":"secret-codex-token"}`),
	}}}
	dispatcher.lastResults = []PlanResult{{
		ID:       "codex-index",
		Provider: ProviderCodex,
		Label:    "rayen@example.test",
		Source:   AuthSourceCPA,
		Plan:     "oauth",
	}}

	raw, err := dispatcher.Call("management.handle", []byte(`{"Method":"GET","Path":"/v0/resource/plugins/quota-center/status"}`))
	if err != nil {
		t.Fatalf("management.handle error = %v", err)
	}
	var response struct {
		StatusCode int
		Body       []byte
	}
	if err := decodeRPCResult(raw, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	body := string(response.Body)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "暂无连接") {
		t.Fatalf("public resource response = %d %s", response.StatusCode, body)
	}
	for _, leaked := range []string{"codex-index", "codex.json", "secret-codex-token", "rayen@example.test", "CPA 认证"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("public resource leaked cached management data %q", leaked)
		}
	}
}

func TestRPCManagementPanelPathResolvesNativeCredentials(t *testing.T) {
	dispatcher := NewTestRPC()
	if _, err := dispatcher.Call("plugin.register", nil); err != nil {
		t.Fatalf("plugin.register error = %v", err)
	}
	dispatcher.auth = fakeNativeAuthStore{entries: []StoredCredential{{
		AuthIndex: "grok-index",
		Provider:  ProviderGrok,
		Name:      "xai.json",
		Label:     "Grok Account",
		JSON:      json.RawMessage(`{"type":"xai","access_token":"secret-grok-token"}`),
	}}}
	dispatcher.lastResults = []PlanResult{{ID: "cached", Provider: ProviderGrok, Label: "cached"}}

	request := pluginManagementRequest(http.MethodGet, managementStatusPath)
	request.Query = url.Values{"view": []string{"accounts"}}
	raw, err := dispatcher.Call("management.handle", mustTestJSON(t, request))
	if err != nil {
		t.Fatalf("management.handle error = %v", err)
	}
	var response struct {
		StatusCode int
		Body       []byte
	}
	if err := decodeRPCResult(raw, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	body := string(response.Body)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Grok Account") || !strings.Contains(body, "CPA 认证") {
		t.Fatalf("management panel response = %d %s", response.StatusCode, body)
	}
	for _, leaked := range []string{"secret-grok-token", "xai.json", "cached"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("management panel leaked %q", leaked)
		}
	}
}

func TestRPCManagementRoutesAcceptFullGatewayPath(t *testing.T) {
	dispatcher := NewTestRPC()
	if _, err := dispatcher.Call("plugin.register", nil); err != nil {
		t.Fatalf("plugin.register error = %v", err)
	}

	request := pluginManagementRequest(http.MethodGet, "/v0/management"+managementStatusPath)
	request.Query = url.Values{"view": []string{"accounts"}}
	raw, err := dispatcher.Call("management.handle", mustTestJSON(t, request))
	if err != nil {
		t.Fatalf("management.handle status error = %v", err)
	}
	var response struct {
		StatusCode int
		Body       []byte
	}
	if err := decodeRPCResult(raw, &response); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(response.Body), "额度中心") {
		t.Fatalf("full management status response = %d %s", response.StatusCode, response.Body)
	}

	raw, err = dispatcher.Call("management.handle", mustTestJSON(t, pluginManagementRequest(
		http.MethodPost,
		"/v0/management"+managementAccountsPath,
		[]byte(`{"provider":"zhipu"}`),
	)))
	if err != nil {
		t.Fatalf("management.handle accounts error = %v", err)
	}
	if err := decodeRPCResult(raw, &response); err != nil {
		t.Fatalf("decode accounts response: %v", err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("full management accounts response = %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
}

func TestRPCRejectsUnknownMethod(t *testing.T) {
	_, err := NewTestRPC().Call("nope", nil)
	if err == nil {
		t.Fatal("Call() error = nil, want unknown method")
	}
}

func mustTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test request: %v", err)
	}
	return raw
}

func TestConfigureLoadsSavedFileAccountsWithoutExposingResourceSnapshot(t *testing.T) {
	dir := t.TempDir()
	store := &fileAccountStore{dir: dir}
	credential, _ := json.Marshal(map[string]any{
		"id":       "zhipu-saved-resource",
		"provider": "zhipu",
		"type":     "zhipu",
		"plan":     "api-Key",
		"label":    "已保存资源测试账号",
		"api_key":  "secret-zhipu-key",
	})
	if _, err := store.Save(context.Background(), "zhipu-saved-resource.json", credential); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	var requests int
	dispatcher := NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(http.StatusOK, `{"data":{"level":"api-Key","limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":20}]}}`), nil
	})))
	dispatcher.accounts = store
	dispatcher.auth = fakeNativeAuthStore{}
	if _, err := dispatcher.Call("plugin.register", nil); err != nil {
		t.Fatalf("plugin.register error = %v", err)
	}
	if requests == 0 {
		t.Fatal("install must refresh saved file quotas")
	}
	management := dispatcher.handleManagement(pluginManagementRequest(http.MethodGet, managementStatusPath))
	managementBody := string(management.Body)
	if management.StatusCode != http.StatusOK || !strings.Contains(managementBody, "已保存资源测试账号") || !strings.Contains(managementBody, `data-account-id="zhipu-saved-resource"`) || !strings.Contains(managementBody, "80.0%") {
		t.Fatalf("management after install = %d %s", management.StatusCode, managementBody)
	}
	resource := dispatcher.handleManagement(pluginManagementRequest(http.MethodGet, resourceStatusPath))
	resourceBody := string(resource.Body)
	for _, leaked := range []string{"zhipu-saved-resource", "已保存资源测试账号", "80.0%", "secret-zhipu-key"} {
		if strings.Contains(resourceBody, leaked) {
			t.Fatalf("public resource leaked saved account data %q", leaked)
		}
	}
}
