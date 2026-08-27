package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDecodeConfigAccountsSupportsProvidersAndLegacyPlans(t *testing.T) {
	raw := []byte(`
accounts:
  - id: minimax-main
    provider: minimax
    plan: coding-plan
    label: MiniMax Coding
    api_key_env: MINIMAX_KEY
  - id: ark-agent
    provider: ark
    plan: agent-plan
    access_key_env: ARK_ACCESS
    secret_key_env: ARK_SECRET
plans:
  - id: glm-pro
    label: GLM Pro
    api_key_env: ZHIPU_KEY
`)
	cfg, err := DecodeConfig(raw, func(name string) string {
		return map[string]string{
			"MINIMAX_KEY": "mm-key",
			"ARK_ACCESS":  "ark-access",
			"ARK_SECRET":  "ark-secret",
			"ZHIPU_KEY":   "zhipu-key",
		}[name]
	})
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}
	if len(cfg.Plans) != 3 {
		t.Fatalf("len(Plans) = %d, want 3", len(cfg.Plans))
	}
	if cfg.Plans[0].Provider != ProviderMiniMax || cfg.Plans[0].Plan != "coding-plan" || cfg.Plans[0].APIKey != "mm-key" {
		t.Fatalf("MiniMax account = %#v", cfg.Plans[0])
	}
	if cfg.Plans[1].Provider != ProviderArk || cfg.Plans[1].AccessKey != "ark-access" || cfg.Plans[1].SecretKey != "ark-secret" {
		t.Fatalf("Ark account = %#v", cfg.Plans[1])
	}
	if cfg.Plans[2].Provider != ProviderZhipu {
		t.Fatalf("legacy plan provider = %q, want %q", cfg.Plans[2].Provider, ProviderZhipu)
	}
}

func TestParseMiniMaxQuotaResponseUsesGeneralRemainingWindows(t *testing.T) {
	result, err := ParseMiniMaxQuotaResponse([]byte(`{
      "model_remains": [
        {"model_name":"video","current_interval_remaining_percent":50,"current_weekly_remaining_percent":50},
        {"model_name":"general","current_interval_remaining_percent":98,"current_weekly_remaining_percent":95,"current_weekly_status":1,"end_time":1780329600000,"weekly_end_time":1780848000000}
      ],
      "base_resp":{"status_code":0}
    }`))
	if err != nil {
		t.Fatalf("ParseMiniMaxQuotaResponse() error = %v", err)
	}
	if len(result.Windows) != 2 || result.Windows[0].Name != "five_hour" || result.Windows[0].UsedPercent != 2 || result.Windows[1].UsedPercent != 5 {
		t.Fatalf("Windows = %#v", result.Windows)
	}
}

func TestClientRoutesMiniMaxRegions(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantHost string
		wantPath string
	}{
		{"legacy China", "", "api.minimaxi.com", "/v1/token_plan/remains"},
		{"international", miniMaxGlobalEndpoint, "api.minimax.io", "/v1/api/openplatform/coding_plan/remains"},
		{"China explicit", miniMaxCNEndpoint, "api.minimaxi.com", "/v1/token_plan/remains"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seenHost, seenPath, seenAuth string
			client := NewClient(QuotaHTTPClientFunc(func(_ context.Context, req *http.Request) (*http.Response, error) {
				seenHost, seenPath, seenAuth = req.URL.Host, req.URL.Path, req.Header.Get("Authorization")
				return jsonResponse(http.StatusOK, `{"model_remains":[{"model_name":"general","current_interval_remaining_percent":80}],"base_resp":{"status_code":0}}`), nil
			}))
			_, err := client.FetchQuota(context.Background(), PlanConfig{Provider: ProviderMiniMax, APIKey: "secret-mm", Endpoint: tt.endpoint})
			if err != nil {
				t.Fatalf("FetchQuota() error = %v", err)
			}
			if seenHost != tt.wantHost || seenPath != tt.wantPath || seenAuth != "Bearer secret-mm" {
				t.Fatalf("request = %s %s %q", seenHost, seenPath, seenAuth)
			}
		})
	}
}

func TestParseMiniMaxInternationalRemainingCountAndOffset(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	result, err := parseMiniMaxQuotaResponse([]byte(`{
		"model_remains":[{"model_name":"general","current_interval_total_count":100,"current_interval_usage_count":80,"current_weekly_total_count":200,"current_weekly_usage_count":150,"remains_time":3600000,"weekly_remains_time":7200000}],
		"base_resp":{"status_code":0}
	}`), miniMaxRegionInternational, now)
	if err != nil {
		t.Fatalf("parseMiniMaxQuotaResponse() error = %v", err)
	}
	if result.FiveHour.RemainingPercent != 80 || result.FiveHour.UsedPercent != 20 || !result.FiveHour.ResetAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("five-hour = %#v", result.FiveHour)
	}
	if result.Weekly.RemainingPercent != 75 || result.Weekly.UsedPercent != 25 || !result.Weekly.ResetAt.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("weekly = %#v", result.Weekly)
	}
}

func TestParseMiniMaxInternationalPercentageFallback(t *testing.T) {
	result, err := parseMiniMaxQuotaResponse([]byte(`{
		"model_remains":[{"model_name":"general","current_interval_remaining_percent":125,"current_weekly_remaining_percent":-5,"current_weekly_status":1}],
		"base_resp":{"status_code":0}
	}`), miniMaxRegionInternational, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatalf("parseMiniMaxQuotaResponse() error = %v", err)
	}
	if result.FiveHour.RemainingPercent != 100 || result.FiveHour.UsedPercent != 0 || result.Weekly.RemainingPercent != 0 || result.Weekly.UsedPercent != 100 {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseMiniMaxChinaUsedCountAndMissingWeekly(t *testing.T) {
	result, err := parseMiniMaxQuotaResponse([]byte(`{
		"model_remains":[{"model_name":"MiniMax-M2.7","current_interval_total_count":100,"current_interval_usage_count":80,"remains_time":3600000}],
		"base_resp":{"status_code":0}
	}`), miniMaxRegionChina, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatalf("parseMiniMaxQuotaResponse() error = %v", err)
	}
	if len(result.Windows) != 1 || result.FiveHour.RemainingPercent != 20 || result.FiveHour.UsedPercent != 80 || result.Weekly.Name != "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseMiniMaxRejectsNonzeroBaseResponse(t *testing.T) {
	_, err := parseMiniMaxQuotaResponse([]byte(`{"base_resp":{"status_code":1001,"status_msg":"bad credential"}}`), miniMaxRegionInternational, time.Unix(100, 0).UTC())
	if err == nil || !strings.Contains(err.Error(), "1001") {
		t.Fatalf("parseMiniMaxQuotaResponse() error = %v", err)
	}
}

func TestParseArkQuotaResponsesExposeAgentAndCodingWindows(t *testing.T) {
	agent, err := ParseArkAgentQuotaResponse([]byte(`{"Result":{"PlanType":"pro","AFPFiveHour":{"Quota":100,"Used":25,"ResetTime":1780329600},"AFPWeekly":{"Quota":500,"Used":50,"ResetTime":1780848000},"AFPMonthly":{"Quota":1000,"Used":100,"ResetTime":1783008000}}}`))
	if err != nil {
		t.Fatalf("ParseArkAgentQuotaResponse() error = %v", err)
	}
	if len(agent.Windows) != 3 || agent.Level != "Agent Plan pro" || agent.Windows[0].UsedPercent != 25 {
		t.Fatalf("agent = %#v", agent)
	}

	coding, err := ParseArkCodingQuotaResponse([]byte(`{"Result":{"QuotaUsage":[{"Level":"session","Percent":12,"ResetTimestamp":1780329600},{"Level":"weekly","Percent":22,"ResetTimestamp":1780848000},{"Level":"monthly","Percent":30,"ResetTimestamp":1783008000}]}}`))
	if err != nil {
		t.Fatalf("ParseArkCodingQuotaResponse() error = %v", err)
	}
	if len(coding.Windows) != 3 || coding.Level != "Coding Plan" || coding.Windows[1].Name != "weekly" {
		t.Fatalf("coding = %#v", coding)
	}
}

func TestParseCodexAndGeminiQuotaResponses(t *testing.T) {
	codex, err := ParseCodexQuotaResponse([]byte(`{"rate_limit":{"primary_window":{"used_percent":20,"limit_window_seconds":18000,"reset_at":1780329600},"secondary_window":{"used_percent":40,"limit_window_seconds":604800,"reset_at":1780848000}}}`))
	if err != nil {
		t.Fatalf("ParseCodexQuotaResponse() error = %v", err)
	}
	if len(codex.Windows) != 2 || codex.Windows[0].Name != "five_hour" || codex.Windows[1].Name != "seven_day" {
		t.Fatalf("codex = %#v", codex)
	}

	gemini, err := ParseGeminiQuotaResponse([]byte(`{"buckets":[{"modelId":"gemini-2.5-pro","remainingFraction":0.8,"resetTime":"2026-08-16T00:00:00Z"},{"modelId":"gemini-2.5-flash","remainingFraction":0.5,"resetTime":"2026-08-16T01:00:00Z"},{"modelId":"gemini-2.5-pro","remainingFraction":0.6,"resetTime":"2026-08-16T00:30:00Z"}]}`))
	if err != nil {
		t.Fatalf("ParseGeminiQuotaResponse() error = %v", err)
	}
	if len(gemini.Windows) != 2 || gemini.Windows[0].Name != "pro" || gemini.Windows[0].UsedPercent != 40 || gemini.Windows[1].UsedPercent != 50 {
		t.Fatalf("gemini = %#v", gemini)
	}
}

func TestParseAntigravityQuotaSummaryUsesFamilyWindows(t *testing.T) {
	result, err := ParseAntigravityQuotaSummary([]byte(`{
		"groups":[
			{"displayName":"Gemini models","buckets":[
				{"displayName":"Five Hour Limit Remaining","window":"5h","remainingFraction":1,"resetTime":"2026-08-16T04:20:00Z"},
				{"displayName":"Weekly Limit Remaining","window":"weekly","remainingFraction":0.97,"resetTime":"2026-08-21T08:00:00Z"}
			]},
			{"displayName":"Claude and GPT models","buckets":[
				{"displayName":"Five Hour Limit Remaining","window":"5h","resetTime":"2026-08-16T05:00:00Z"},
				{"displayName":"Weekly Limit Remaining","window":"weekly","resetTime":"2026-08-23T00:00:00Z"}
			]}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseAntigravityQuotaSummary() error = %v", err)
	}
	if result.Level != "Antigravity" || len(result.Windows) != 4 {
		t.Fatalf("result = %#v", result)
	}
	if result.Windows[0].Group != "Gemini 模型" || result.Windows[0].Name != "Five Hour Limit Remaining" || result.Windows[0].RemainingPercent != 100 || result.Windows[0].Available {
		t.Fatalf("gemini 5h = %#v", result.Windows[0])
	}
	if result.Windows[1].Group != "Gemini 模型" || result.Windows[1].RemainingPercent != 97 {
		t.Fatalf("gemini weekly = %#v", result.Windows[1])
	}
	if result.Windows[2].Group != "Claude 和 GPT 模型" || !result.Windows[2].Available || result.Windows[3].Group != "Claude 和 GPT 模型" || !result.Windows[3].Available {
		t.Fatalf("claude windows = %#v", result.Windows[2:])
	}
}

func TestFetchGeminiUsesAntigravitySummaryForAntigravityAuth(t *testing.T) {
	var seen []string
	client := NewClient(QuotaHTTPClientFunc(func(ctx context.Context, req *http.Request) (*http.Response, error) {
		seen = append(seen, req.URL.Host+req.URL.Path)
		if strings.Contains(req.URL.Path, "retrieveUserQuotaSummary") {
			if req.Header.Get("User-Agent") == "" || !strings.Contains(req.Header.Get("User-Agent"), "antigravity") {
				t.Fatalf("missing antigravity user agent: %q", req.Header.Get("User-Agent"))
			}
			return jsonResponse(http.StatusOK, `{"groups":[{"displayName":"Gemini models","buckets":[{"displayName":"Five Hour Limit Remaining","window":"5h","remainingFraction":1,"resetTime":"2026-08-16T04:20:00Z"},{"displayName":"Weekly Limit Remaining","window":"weekly","remainingFraction":0.97,"resetTime":"2026-08-21T08:00:00Z"}]},{"displayName":"Claude and GPT models","buckets":[{"displayName":"Five Hour Limit Remaining","window":"5h","resetTime":"2026-08-16T05:00:00Z"},{"displayName":"Weekly Limit Remaining","window":"weekly","resetTime":"2026-08-23T00:00:00Z"}]}]}`), nil
		}
		t.Fatalf("unexpected request %s", req.URL)
		return jsonResponse(http.StatusNotFound, `{}`), nil
	}))
	result, err := client.FetchQuota(context.Background(), PlanConfig{
		ID:          "antigravity-user",
		Provider:    ProviderGemini,
		AuthType:    "antigravity",
		AccessToken: "secret-token",
		AccountID:   "proj-1",
	})
	if err != nil {
		t.Fatalf("FetchQuota() error = %v", err)
	}
	if result == nil || len(result.Windows) != 4 || result.Windows[0].Group != "Gemini 模型" || result.Windows[2].Group != "Claude 和 GPT 模型" {
		t.Fatalf("result = %#v", result)
	}
	if len(seen) == 0 || !strings.Contains(seen[0], "retrieveUserQuotaSummary") {
		t.Fatalf("seen = %#v", seen)
	}
}

func TestFetchAntigravityUsesOnlyStableEndpoint(t *testing.T) {
	client := NewClient(QuotaHTTPClientFunc(func(ctx context.Context, req *http.Request) (*http.Response, error) {
		if req.URL.Host != "cloudcode-pa.googleapis.com" {
			t.Fatalf("non-stable endpoint called: %s", req.URL)
		}
		return jsonResponse(http.StatusOK, `{"groups":[{"displayName":"Gemini models","buckets":[{"displayName":"Weekly","window":"weekly","remainingFraction":0.5}]}]}`), nil
	}))

	result, err := client.FetchQuota(context.Background(), PlanConfig{
		ID: "antigravity", Provider: ProviderGemini, AuthType: "antigravity", AccessToken: "secret-token",
	})
	if err != nil {
		t.Fatalf("FetchQuota() error = %v", err)
	}
	if result == nil || len(result.Windows) != 1 || result.Windows[0].RemainingPercent != 50 {
		t.Fatalf("result = %#v", result)
	}
}

func TestFetchGeminiReportsAntigravityFallbackFailure(t *testing.T) {
	client := NewClient(QuotaHTTPClientFunc(func(ctx context.Context, req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "retrieveUserQuotaSummary") {
			return jsonResponse(http.StatusForbidden, `{"error":"antigravity denied"}`), nil
		}
		return jsonResponse(http.StatusForbidden, `{"error":"gemini denied"}`), nil
	}))

	_, err := client.FetchQuota(context.Background(), PlanConfig{
		ID: "antigravity", Provider: ProviderGemini, AuthType: "antigravity", AccessToken: "secret-token",
	})
	if err == nil || !strings.Contains(err.Error(), "Antigravity API 403") || !strings.Contains(err.Error(), "Gemini API 403") {
		t.Fatalf("FetchQuota() error = %v, want merged Antigravity and Gemini failures", err)
	}
}

func TestClientFetchesMiniMaxWithBearerAndDoesNotLeakKey(t *testing.T) {
	var seenPath, seenAuth string
	client := NewClient(QuotaHTTPClientFunc(func(ctx context.Context, req *http.Request) (*http.Response, error) {
		seenPath = req.URL.Path
		seenAuth = req.Header.Get("Authorization")
		return jsonResponse(http.StatusOK, `{"model_remains":[{"model_name":"MiniMax-M2.7","current_interval_total_count":100,"current_interval_usage_count":10,"current_weekly_status":3}]}`), nil
	}))
	result, err := client.FetchQuota(context.Background(), PlanConfig{ID: "mm", Provider: ProviderMiniMax, APIKey: "secret-mm"})
	if err != nil {
		t.Fatalf("FetchQuota() error = %v", err)
	}
	if seenPath != "/v1/token_plan/remains" || seenAuth != "Bearer secret-mm" {
		t.Fatalf("request path/auth = %q / %q", seenPath, seenAuth)
	}
	if result == nil || len(result.Windows) != 1 || result.Windows[0].UsedPercent != 10 {
		t.Fatalf("result = %#v", result)
	}
	if strings.Contains(string(mustJSON(result)), "secret-mm") {
		t.Fatal("quota result leaked API key")
	}
}

func TestRenderPanelProvidesNavigationFourColumnCardsAndAddWindow(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	html := RenderPanel([]PlanResult{
		{ID: "zhipu", Provider: ProviderZhipu, Label: "智谱", Quota: &QuotaResult{Windows: []QuotaWindow{{Name: "five_hour", RemainingPercent: 80}}}, FetchedAt: at},
		{ID: "grok", Provider: ProviderGrok, Label: "Grok", Error: "实时额度不可读取", FetchedAt: at},
	}, at)
	for _, want := range []string{"总览", "账号配置", "四列", "添加连接", "供应商", "Grok", "实时额度不可读取", "refresh"} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing %q", want)
		}
	}
	if strings.Contains(html, "secret-mm") || strings.Contains(html, "api_key") {
		t.Fatal("panel leaked credential material")
	}
}

func TestStatusJSONCarriesProviderAndWindows(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	raw := RenderStatusJSON([]PlanResult{{ID: "gemini", Provider: ProviderGemini, Label: "Gemini", Quota: &QuotaResult{Windows: []QuotaWindow{{Name: "pro", RemainingPercent: 60}}}, FetchedAt: at}})
	var payload struct {
		Plans []struct {
			Provider string `json:"provider"`
			Quota    struct {
				Windows []QuotaWindow `json:"windows"`
			} `json:"quota"`
		} `json:"plans"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if len(payload.Plans) != 1 || payload.Plans[0].Provider != ProviderGemini || len(payload.Plans[0].Quota.Windows) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestFetchArkFallsBackToCodingPlanAfterAgentForbidden(t *testing.T) {
	var actions []string
	client := NewClient(QuotaHTTPClientFunc(func(ctx context.Context, req *http.Request) (*http.Response, error) {
		action := req.URL.Query().Get("Action")
		actions = append(actions, action)
		if action == "GetAFPUsage" {
			return jsonResponse(http.StatusForbidden, `{"ResponseMetadata":{"Error":{"Code":"AccessDenied","Message":"no agent plan"}}}`), nil
		}
		return jsonResponse(http.StatusOK, `{"Result":{"QuotaUsage":[{"Level":"weekly","Percent":22,"ResetTimestamp":1780848000}]}}`), nil
	}))
	result, err := client.FetchQuota(context.Background(), PlanConfig{ID: "ark", Provider: ProviderArk, AccessKey: "ak-test", SecretKey: "sk-test"})
	if err != nil {
		t.Fatalf("FetchQuota() error = %v", err)
	}
	if strings.Join(actions, ",") != "GetAFPUsage,GetCodingPlanUsage" {
		t.Fatalf("actions = %v", actions)
	}
	if result == nil || len(result.Windows) != 1 || result.Windows[0].Name != "weekly" {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseArkResponsesRejectMetadataError(t *testing.T) {
	body := []byte(`{"ResponseMetadata":{"Error":{"Code":"SignatureDoesNotMatch","Message":"The request signature we calculated does not match"}}}`)
	if _, err := ParseArkAgentQuotaResponse(body); err == nil || !strings.Contains(err.Error(), "SignatureDoesNotMatch") {
		t.Fatalf("ParseArkAgentQuotaResponse() error = %v", err)
	}
	if _, err := ParseArkCodingQuotaResponse(body); err == nil || !strings.Contains(err.Error(), "SignatureDoesNotMatch") {
		t.Fatalf("ParseArkCodingQuotaResponse() error = %v", err)
	}
}
