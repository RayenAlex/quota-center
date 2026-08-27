package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	miniMaxCNEndpoint           = "https://api.minimaxi.com/v1/token_plan/remains"
	miniMaxGlobalEndpoint       = "https://api.minimax.io/v1/api/openplatform/coding_plan/remains"
	codexUsageEndpoint          = "https://chatgpt.com/backend-api/wham/usage"
	geminiCodeAssist            = "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	geminiQuotaEndpoint         = "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota"
	antigravityDefaultProjectID = "bamboo-precept-lgxtn"
	antigravityUserAgent        = "antigravity/cli/1.0.13 (aidev_client; os_type=darwin; arch=arm64)"
	arkOpenAPIHost              = "open.volcengineapi.com"
	arkOpenAPIVersion           = "2024-01-01"
	arkDefaultRegion            = "cn-beijing"
	arkService                  = "ark"
	arkContentType              = "application/json; charset=utf-8"
	arkSignedHeaders            = "host;x-date;x-content-sha256;content-type"
)

type miniMaxRegion string

const (
	miniMaxRegionInternational miniMaxRegion = "international"
	miniMaxRegionChina         miniMaxRegion = "china"
)

func providerLabel(provider Provider) string {
	switch provider {
	case ProviderZhipu:
		return "智谱"
	case ProviderMiniMax:
		return "MiniMax"
	case ProviderOpenCode:
		return "OpenCode Go"
	case ProviderArk:
		return "方舟"
	case ProviderGrok:
		return "Grok"
	case ProviderCodex:
		return "Codex"
	case ProviderGemini:
		return "Gemini"
	default:
		return string(provider)
	}
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string, headers map[string]string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := c.httpDo(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		response.Body.Close()
		return nil, response.StatusCode, fmt.Errorf("quota endpoint returned a redirect")
	}
	defer response.Body.Close()
	raw, err := readQuotaResponse(response.Body)
	if err != nil {
		return nil, response.StatusCode, err
	}
	return raw, response.StatusCode, nil
}

func (c *Client) fetchMiniMax(ctx context.Context, plan PlanConfig) (*QuotaResult, error) {
	endpoint := miniMaxCNEndpoint
	if plan.Endpoint != "" {
		endpoint = plan.Endpoint
	}
	key := strings.TrimSpace(plan.APIKey)
	raw, status, err := c.doRequest(ctx, http.MethodGet, endpoint, map[string]string{
		"Authorization": "Bearer " + key,
		"Content-Type":  "application/json",
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("MiniMax request quota: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("MiniMax API %d: %s", status, sanitizeMessage(string(raw)))
	}
	result, err := parseMiniMaxQuotaResponse(raw, miniMaxRegionForEndpoint(endpoint), time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("decode MiniMax quota response: %w", err)
	}
	return result, nil
}

type miniMaxQuotaModel struct {
	ModelName                string   `json:"model_name"`
	IntervalRemainingPercent *float64 `json:"current_interval_remaining_percent"`
	WeeklyRemainingPercent   *float64 `json:"current_weekly_remaining_percent"`
	WeeklyStatus             *int     `json:"current_weekly_status"`
	IntervalUsageCount       *float64 `json:"current_interval_usage_count"`
	IntervalTotalCount       *float64 `json:"current_interval_total_count"`
	WeeklyUsageCount         *float64 `json:"current_weekly_usage_count"`
	WeeklyTotalCount         *float64 `json:"current_weekly_total_count"`
	EndTime                  int64    `json:"end_time"`
	WeeklyEndTime            int64    `json:"weekly_end_time"`
	RemainsTime              int64    `json:"remains_time"`
	WeeklyRemainsTime        int64    `json:"weekly_remains_time"`
}

type miniMaxQuotaResponse struct {
	ModelRemains []miniMaxQuotaModel `json:"model_remains"`
	BaseResp     *struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp,omitempty"`
}

func ParseMiniMaxQuotaResponse(body []byte) (*QuotaResult, error) {
	var payload miniMaxQuotaResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.BaseResp != nil && payload.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("API error (code %d): %s", payload.BaseResp.StatusCode, payload.BaseResp.StatusMsg)
	}
	for i := range payload.ModelRemains {
		if payload.ModelRemains[i].ModelName == "general" {
			return parseMiniMaxQuotaModel(&payload.ModelRemains[i], miniMaxRegionInternational, time.Now().UTC()), nil
		}
	}
	return &QuotaResult{Level: "Coding Plan"}, nil
}

func miniMaxRegionForEndpoint(endpoint string) miniMaxRegion {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err == nil && strings.EqualFold(parsed.Hostname(), "api.minimax.io") {
		return miniMaxRegionInternational
	}
	return miniMaxRegionChina
}

func parseMiniMaxQuotaResponse(body []byte, region miniMaxRegion, now time.Time) (*QuotaResult, error) {
	var payload miniMaxQuotaResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.BaseResp != nil && payload.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("API error (code %d): %s", payload.BaseResp.StatusCode, payload.BaseResp.StatusMsg)
	}
	item := selectMiniMaxModel(payload.ModelRemains, region)
	return parseMiniMaxQuotaModel(item, region, now), nil
}

func parseMiniMaxQuotaModel(item *miniMaxQuotaModel, region miniMaxRegion, now time.Time) *QuotaResult {
	result := &QuotaResult{Level: "Coding Plan"}
	if item == nil {
		return result
	}
	if remaining, ok := miniMaxRemainingPercent(item.IntervalRemainingPercent, item.IntervalUsageCount, item.IntervalTotalCount, region); ok {
		window := QuotaWindow{Name: "five_hour", RemainingPercent: remaining, UsedPercent: clamp(100 - remaining), ResetAt: miniMaxResetTime(item.EndTime, item.RemainsTime, now)}
		result.FiveHour = window
		result.Windows = append(result.Windows, window)
	}
	if miniMaxWeeklyAvailable(item) {
		if remaining, ok := miniMaxRemainingPercent(item.WeeklyRemainingPercent, item.WeeklyUsageCount, item.WeeklyTotalCount, region); !ok {
			return result
		} else {
			weekly := QuotaWindow{Name: "weekly", RemainingPercent: remaining, UsedPercent: clamp(100 - remaining), ResetAt: miniMaxResetTime(item.WeeklyEndTime, item.WeeklyRemainsTime, now)}
			result.Weekly = weekly
			result.Windows = append(result.Windows, weekly)
		}
	}
	return result
}

func miniMaxRemainingPercent(percentage, usage, total *float64, region miniMaxRegion) (float64, bool) {
	if total != nil && usage != nil && *total > 0 && *usage >= 0 {
		remaining := *usage / *total * 100
		if region == miniMaxRegionChina {
			remaining = 100 - remaining
		}
		return math.Round(clamp(remaining)*100) / 100, true
	}
	if region == miniMaxRegionInternational && percentage != nil {
		return math.Round(clamp(*percentage)*100) / 100, true
	}
	return 0, false
}

func miniMaxWeeklyAvailable(item *miniMaxQuotaModel) bool {
	if item.WeeklyStatus != nil && *item.WeeklyStatus != 1 {
		return false
	}
	return item.WeeklyRemainingPercent != nil || (item.WeeklyUsageCount != nil && item.WeeklyTotalCount != nil && *item.WeeklyTotalCount > 0)
}

func miniMaxResetTime(absolute, offsetMillis int64, now time.Time) *time.Time {
	if offsetMillis > 0 {
		at := now.Add(time.Duration(offsetMillis) * time.Millisecond).UTC()
		return &at
	}
	if absolute > 0 {
		return epochMillis(absolute)
	}
	return nil
}

func selectMiniMaxModel(items []miniMaxQuotaModel, region miniMaxRegion) *miniMaxQuotaModel {
	var selected *miniMaxQuotaModel
	var wildcard *miniMaxQuotaModel
	best := 101.0
	for i := range items {
		item := &items[i]
		name := strings.ToLower(strings.TrimSpace(item.ModelName))
		allowed := strings.HasPrefix(name, "minimax-m")
		if region == miniMaxRegionInternational {
			allowed = allowed || name == "general" || name == "video"
		}
		if !allowed {
			continue
		}
		remaining, reportable := miniMaxRemainingPercent(item.IntervalRemainingPercent, item.IntervalUsageCount, item.IntervalTotalCount, region)
		if !reportable {
			continue
		}
		if name == "minimax-m*" {
			copy := *item
			wildcard = &copy
			continue
		}
		if selected == nil || remaining < best {
			copy := *item
			selected = &copy
			best = remaining
		}
	}
	if wildcard != nil {
		return wildcard
	}
	return selected
}

func (c *Client) fetchCodex(ctx context.Context, plan PlanConfig) (*QuotaResult, error) {
	token := firstNonEmpty(plan.AccessToken, plan.APIKey)
	raw, status, err := doQuotaHTTP(ctx, c, codexUsageRequest(token, plan.AccountID))
	if err != nil {
		return nil, fmt.Errorf("Codex request quota: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("Codex API %d: %s", status, sanitizeMessage(string(raw)))
	}
	result, err := ParseCodexQuotaResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("decode Codex quota response: %w", err)
	}
	return result, nil
}

type codexQuotaResponse struct {
	RateLimit *struct {
		PrimaryWindow   *codexQuotaWindow `json:"primary_window"`
		SecondaryWindow *codexQuotaWindow `json:"secondary_window"`
	} `json:"rate_limit"`
}

type codexQuotaWindow struct {
	UsedPercent   *float64 `json:"used_percent"`
	WindowSeconds *int64   `json:"limit_window_seconds"`
	ResetAt       *int64   `json:"reset_at"`
}

func ParseCodexQuotaResponse(body []byte) (*QuotaResult, error) {
	var payload codexQuotaResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	result := &QuotaResult{Level: "Codex"}
	if payload.RateLimit == nil {
		return result, nil
	}
	for _, item := range []*codexQuotaWindow{payload.RateLimit.PrimaryWindow, payload.RateLimit.SecondaryWindow} {
		if item == nil || item.UsedPercent == nil {
			continue
		}
		window := QuotaWindow{Name: codexWindowName(item.WindowSeconds), UsedPercent: clamp(*item.UsedPercent), RemainingPercent: clamp(100 - *item.UsedPercent)}
		if item.ResetAt != nil {
			window.ResetAt = epochSeconds(*item.ResetAt)
		}
		result.Windows = append(result.Windows, window)
		if window.Name == "five_hour" {
			result.FiveHour = window
		} else if result.Weekly.Name == "" {
			result.Weekly = window
		}
	}
	return result, nil
}

func codexWindowName(seconds *int64) string {
	if seconds == nil {
		return "rate_limit"
	}
	switch *seconds {
	case 18000:
		return "five_hour"
	case 604800:
		return "seven_day"
	case 2592000:
		return "thirty_day"
	default:
		if *seconds >= 86400 {
			return strconv.FormatInt(*seconds/86400, 10) + "_day"
		}
		return strconv.FormatInt(*seconds/3600, 10) + "_hour"
	}
}

func (c *Client) fetchGemini(ctx context.Context, plan PlanConfig) (*QuotaResult, error) {
	var antigravityErr error
	combineErrors := func(err error) error {
		if antigravityErr == nil {
			return err
		}
		return fmt.Errorf("%v; %w", antigravityErr, err)
	}
	if isAntigravityPlan(plan) {
		result, err := c.fetchAntigravitySummary(ctx, plan)
		if err == nil && result != nil && len(result.Windows) > 0 {
			return result, nil
		}
		antigravityErr = err
	}
	token := firstNonEmpty(plan.AccessToken, plan.APIKey)
	projectID := strings.TrimSpace(plan.AccountID)
	if projectID == "" {
		loadBody, loadStatus, err := doQuotaHTTP(ctx, c, geminiLoadRequest(token))
		if err != nil {
			return nil, combineErrors(fmt.Errorf("Gemini loadCodeAssist: %w", err))
		}
		if loadStatus < 200 || loadStatus >= 300 {
			return nil, combineErrors(fmt.Errorf("Gemini API %d: %s", loadStatus, sanitizeMessage(string(loadBody))))
		}
		projectID = extractGeminiProjectID(loadBody)
	}
	quotaBody, quotaStatus, err := doQuotaHTTP(ctx, c, geminiQuotaRequest(token, projectID))
	if err != nil {
		return nil, combineErrors(fmt.Errorf("Gemini retrieveUserQuota: %w", err))
	}
	if quotaStatus < 200 || quotaStatus >= 300 {
		return nil, combineErrors(fmt.Errorf("Gemini API %d: %s", quotaStatus, sanitizeMessage(string(quotaBody))))
	}
	result, err := ParseGeminiQuotaResponse(quotaBody)
	if err != nil {
		return nil, combineErrors(fmt.Errorf("decode Gemini quota response: %w", err))
	}
	return result, nil
}

func (c *Client) fetchGrok(ctx context.Context, plan PlanConfig) (*QuotaResult, error) {
	token := firstNonEmpty(plan.AccessToken, plan.APIKey)
	raw, status, err := doQuotaHTTP(ctx, c, grokBillingRequest(token))
	if err != nil {
		return nil, fmt.Errorf("Grok experimental quota request: %w", err)
	}
	if status == 16 || (status == 7 && strings.Contains(strings.ToLower(string(raw)), "bad-credentials")) {
		return nil, fmt.Errorf("Grok experimental quota API rejected the CPA credential")
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("Grok experimental quota API %d: %s", status, sanitizeMessage(string(raw)))
	}
	result, err := ParseGrokBillingResponse(raw, time.Now())
	if err != nil {
		// Keep accepting the legacy gRPC-web payload while Grok deployments roll
		// out the CLI billing endpoint response format.
		if legacy, legacyErr := ParseGrokCreditsResponse(raw, time.Now()); legacyErr == nil {
			return legacy, nil
		}
		return nil, fmt.Errorf("decode Grok experimental quota response: %w", err)
	}
	return result, nil
}

type geminiQuotaResponse struct {
	Buckets []struct {
		ModelID           string  `json:"modelId"`
		RemainingFraction float64 `json:"remainingFraction"`
		ResetTime         string  `json:"resetTime"`
	} `json:"buckets"`
}

func ParseGeminiQuotaResponse(body []byte) (*QuotaResult, error) {
	var payload geminiQuotaResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	type bucket struct {
		remaining float64
		reset     string
	}
	grouped := map[string]bucket{}
	for _, item := range payload.Buckets {
		name := geminiWindowName(item.ModelID)
		remaining := clamp(item.RemainingFraction * 100)
		previous, ok := grouped[name]
		if !ok || remaining < previous.remaining {
			grouped[name] = bucket{remaining: remaining, reset: item.ResetTime}
		}
	}
	order := []string{"pro", "flash", "flash-lite"}
	result := &QuotaResult{Level: "Gemini"}
	for _, name := range order {
		item, ok := grouped[name]
		if !ok {
			continue
		}
		window := QuotaWindow{Name: name, RemainingPercent: item.remaining, UsedPercent: clamp(100 - item.remaining)}
		if item.reset != "" {
			if at, err := time.Parse(time.RFC3339, item.reset); err == nil {
				window.ResetAt = &at
			}
		}
		result.Windows = append(result.Windows, window)
	}
	return result, nil
}

func geminiWindowName(model string) string {
	model = strings.ToLower(model)
	if strings.Contains(model, "flash-lite") {
		return "flash-lite"
	}
	if strings.Contains(model, "flash") {
		return "flash"
	}
	if strings.Contains(model, "pro") {
		return "pro"
	}
	return model
}

func isAntigravityPlan(plan PlanConfig) bool {
	for _, value := range []string{plan.AuthType, plan.ID, plan.Label} {
		if strings.Contains(strings.ToLower(value), "antigravity") {
			return true
		}
	}
	return false
}

func antigravitySummaryURLs() []string {
	return []string{
		"https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
	}
}

func (c *Client) fetchAntigravitySummary(ctx context.Context, plan PlanConfig) (*QuotaResult, error) {
	token := firstNonEmpty(plan.AccessToken, plan.APIKey)
	projectID := strings.TrimSpace(plan.AccountID)
	if projectID == "" {
		projectID = antigravityDefaultProjectID
	}
	var lastErr error
	for _, url := range antigravitySummaryURLs() {
		body, status, err := doQuotaHTTP(ctx, c, antigravitySummaryRequest(token, projectID, url))
		if err != nil {
			lastErr = err
			continue
		}
		if status < 200 || status >= 300 {
			lastErr = fmt.Errorf("Antigravity API %d: %s", status, sanitizeMessage(string(body)))
			continue
		}
		result, err := ParseAntigravityQuotaSummary(body)
		if err != nil {
			lastErr = err
			continue
		}
		if len(result.Windows) == 0 {
			lastErr = fmt.Errorf("Antigravity quota summary was empty")
			continue
		}
		return result, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("Antigravity quota summary unavailable")
	}
	return nil, lastErr
}

type antigravityQuotaSummary struct {
	Groups []struct {
		DisplayName  string `json:"displayName"`
		DisplayName2 string `json:"display_name"`
		Buckets      []struct {
			DisplayName        string   `json:"displayName"`
			DisplayName2       string   `json:"display_name"`
			Window             string   `json:"window"`
			ResetTime          string   `json:"resetTime"`
			ResetTime2         string   `json:"reset_time"`
			RemainingFraction  *float64 `json:"remainingFraction"`
			RemainingFraction2 *float64 `json:"remaining_fraction"`
		} `json:"buckets"`
	} `json:"groups"`
}

func ParseAntigravityQuotaSummary(body []byte) (*QuotaResult, error) {
	var payload antigravityQuotaSummary
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	result := &QuotaResult{Level: "Antigravity"}
	for _, group := range payload.Groups {
		groupLabel := antigravityGroupLabel(firstNonEmpty(group.DisplayName, group.DisplayName2))
		for _, bucket := range group.Buckets {
			window := QuotaWindow{
				Group: groupLabel,
				Name:  antigravityBucketName(firstNonEmpty(bucket.DisplayName, bucket.DisplayName2), bucket.Window),
			}
			fraction := bucket.RemainingFraction
			if fraction == nil {
				fraction = bucket.RemainingFraction2
			}
			if fraction == nil {
				window.Available = true
				window.RemainingPercent = 100
			} else {
				window.RemainingPercent = clamp(*fraction * 100)
				window.UsedPercent = clamp(100 - window.RemainingPercent)
			}
			resetRaw := firstNonEmpty(bucket.ResetTime, bucket.ResetTime2)
			if resetRaw != "" {
				if at, err := time.Parse(time.RFC3339, resetRaw); err == nil {
					window.ResetAt = &at
				} else if at, err := time.Parse(time.RFC3339Nano, resetRaw); err == nil {
					window.ResetAt = &at
				}
			}
			result.Windows = append(result.Windows, window)
		}
	}
	return result, nil
}

func antigravityGroupLabel(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(normalized, "claude") || strings.Contains(normalized, "gpt"):
		return "Claude 和 GPT 模型"
	case strings.Contains(normalized, "gemini"):
		return "Gemini 模型"
	case raw != "":
		return raw
	default:
		return "额度"
	}
}

func antigravityBucketName(label, window string) string {
	if strings.TrimSpace(label) != "" {
		return strings.TrimSpace(label)
	}
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "5h", "five-hour", "five_hour", "fivehour":
		return "Five Hour Limit Remaining"
	case "weekly", "week":
		return "Weekly Limit Remaining"
	default:
		return firstNonEmpty(window, "额度")
	}
}

func extractGeminiProjectID(body []byte) string {
	var payload struct {
		Project json.RawMessage `json:"cloudaicompanionProject"`
	}
	if json.Unmarshal(body, &payload) != nil || len(payload.Project) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(payload.Project, &value) == nil {
		return value
	}
	var object struct {
		ID        string `json:"id"`
		ProjectID string `json:"projectId"`
	}
	if json.Unmarshal(payload.Project, &object) == nil {
		return firstNonEmpty(object.ID, object.ProjectID)
	}
	return ""
}

type protoFieldValue struct {
	path   []int
	wire   int
	varint uint64
	fixed  uint32
}

func ParseGrokCreditsResponse(body []byte, now time.Time) (*QuotaResult, error) {
	payload := grokGRPCData(body)
	values := collectProtoFields(payload, nil, 0)
	if len(values) == 0 {
		return nil, fmt.Errorf("Grok experimental response format is not recognized")
	}

	var used *float64
	var usedPath []int
	for _, item := range values {
		if item.wire != 5 || len(item.path) == 0 || item.path[len(item.path)-1] != 1 || item.fixed > 100 {
			continue
		}
		value := float64(item.fixed)
		if used == nil || len(item.path) < len(usedPath) || (len(item.path) == len(usedPath) && earlierPath(item.path, usedPath)) {
			used, usedPath = &value, item.path
		}
	}

	var reset *time.Time
	resetPath := []int(nil)
	for _, item := range values {
		if item.wire != 0 || item.varint < uint64(1.7e9) || item.varint > uint64(2.1e9) {
			continue
		}
		at := time.Unix(int64(item.varint), 0).UTC()
		if !at.After(now) {
			continue
		}
		preferred := len(item.path) == 3 && item.path[0] == 1 && item.path[1] == 5 && item.path[2] == 1
		currentPreferred := len(resetPath) == 3 && resetPath[0] == 1 && resetPath[1] == 5 && resetPath[2] == 1
		if reset == nil || (preferred && !currentPreferred) || (!preferred && !currentPreferred && at.Before(*reset)) {
			reset, resetPath = &at, item.path
		}
	}
	hasUsagePeriod := len(resetPath) >= 2 && resetPath[0] == 1 && resetPath[1] == 5
	if used == nil && (!hasUsagePeriod || reset == nil) {
		return nil, fmt.Errorf("Grok experimental response does not contain a recognizable credits window")
	}

	usedPercent := 0.0
	if used != nil {
		usedPercent = clamp(*used)
	}
	window := QuotaWindow{Name: "credits", UsedPercent: usedPercent, RemainingPercent: clamp(100 - usedPercent), ResetAt: reset}
	return &QuotaResult{Level: "Grok", Windows: []QuotaWindow{window}}, nil
}

func grokGRPCData(body []byte) []byte {
	var data []byte
	for offset := 0; offset+5 <= len(body); {
		flags := body[offset]
		length := int(body[offset+1])<<24 | int(body[offset+2])<<16 | int(body[offset+3])<<8 | int(body[offset+4])
		offset += 5
		if length < 0 || offset+length > len(body) {
			return body
		}
		if flags&0x80 == 0 {
			data = append(data, body[offset:offset+length]...)
		}
		offset += length
	}
	if len(data) == 0 {
		return body
	}
	return data
}

func collectProtoFields(data []byte, parent []int, depth int) []protoFieldValue {
	if depth > 4 {
		return nil
	}
	var values []protoFieldValue
	offset := 0
	for offset < len(data) {
		key, next, ok := protoVarint(data, offset)
		if !ok || key>>32 != 0 {
			return values
		}
		offset = next
		number := int(key >> 3)
		wire := int(key & 7)
		if number <= 0 {
			return values
		}
		path := append(append([]int(nil), parent...), number)
		switch wire {
		case 0:
			value, next, ok := protoVarint(data, offset)
			if !ok {
				return values
			}
			offset = next
			values = append(values, protoFieldValue{path: path, wire: wire, varint: value})
		case 1:
			if offset+8 > len(data) {
				return values
			}
			offset += 8
		case 2:
			length, next, ok := protoVarint(data, offset)
			if !ok || length > uint64(len(data)-next) {
				return values
			}
			nested := data[next : next+int(length)]
			offset = next + int(length)
			if len(nested) > 0 {
				values = append(values, collectProtoFields(nested, path, depth+1)...)
			}
		case 5:
			if offset+4 > len(data) {
				return values
			}
			value := uint32(data[offset]) | uint32(data[offset+1])<<8 | uint32(data[offset+2])<<16 | uint32(data[offset+3])<<24
			offset += 4
			values = append(values, protoFieldValue{path: path, wire: wire, fixed: value})
		default:
			return values
		}
	}
	return values
}

func protoVarint(data []byte, offset int) (uint64, int, bool) {
	var value uint64
	for shift := uint(0); offset < len(data) && shift < 64; shift += 7 {
		item := data[offset]
		offset++
		value |= uint64(item&0x7f) << shift
		if item&0x80 == 0 {
			return value, offset, true
		}
	}
	return 0, offset, false
}

func earlierPath(left, right []int) bool {
	for index := 0; index < len(left) && index < len(right); index++ {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}
	return len(left) < len(right)
}

func (c *Client) fetchArk(ctx context.Context, plan PlanConfig) (*QuotaResult, error) {
	if strings.TrimSpace(plan.AccessKey) == "" || strings.TrimSpace(plan.SecretKey) == "" {
		return nil, fmt.Errorf("方舟额度查询需要 AccessKey ID 和 Secret AccessKey")
	}
	region := arkRegion(plan.Endpoint)
	var failures []string
	raw, status, err := c.fetchArkAction(ctx, region, plan.AccessKey, plan.SecretKey, "GetAFPUsage")
	if err != nil {
		failures = append(failures, err.Error())
	} else if result, parseErr := ParseArkAgentQuotaResponse(raw); parseErr == nil && len(result.Windows) > 0 {
		return result, nil
	} else if apiErr := arkResponseError(raw, status, parseErr); apiErr != nil {
		failures = append(failures, apiErr.Error())
	}
	raw, status, err = c.fetchArkAction(ctx, region, plan.AccessKey, plan.SecretKey, "GetCodingPlanUsage")
	if err != nil {
		return nil, arkJoinErrors(failures, err)
	}
	result, parseErr := ParseArkCodingQuotaResponse(raw)
	if parseErr == nil {
		return result, nil
	}
	return nil, arkJoinErrors(failures, arkResponseError(raw, status, parseErr))
}

func arkJoinErrors(failures []string, err error) error {
	if err == nil && len(failures) == 0 {
		return nil
	}
	if err == nil {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	if len(failures) == 0 {
		return err
	}
	return fmt.Errorf("%s; %v", strings.Join(failures, "; "), err)
}

func arkResponseError(raw []byte, status int, parseErr error) error {
	if code, message := arkMetadataError(raw); code != "" {
		if arkCredentialError(code) {
			return fmt.Errorf("方舟 API 凭据无效: %s", firstNonEmpty(message, code))
		}
		return fmt.Errorf("方舟 API %s: %s", code, firstNonEmpty(message, sanitizeMessage(string(raw))))
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("方舟 API %d: 凭据无效", status)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("方舟 API %d: %s", status, sanitizeMessage(string(raw)))
	}
	if parseErr != nil {
		return parseErr
	}
	return nil
}

func arkMetadataError(raw []byte) (string, string) {
	var payload struct {
		ResponseMetadata struct {
			Error *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"ResponseMetadata"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.ResponseMetadata.Error == nil {
		return "", ""
	}
	return strings.TrimSpace(payload.ResponseMetadata.Error.Code), strings.TrimSpace(payload.ResponseMetadata.Error.Message)
}

func arkCredentialError(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "signaturedoesnotmatch", "invalidaccesskey", "invalidaccesskeyid", "authfailure", "invalidauthorization", "invalidsecrettoken":
		return true
	default:
		return false
	}
}

func (c *Client) fetchArkAction(ctx context.Context, region, accessKey, secretKey, action string) ([]byte, int, error) {
	query := arkCanonicalQuery(action, region)
	now := time.Now().UTC()
	authorization, xDate, contentHash := arkSign(accessKey, secretKey, region, query, nil, now)
	return c.doRequest(ctx, http.MethodPost, "https://"+arkOpenAPIHost+"/?"+query, map[string]string{
		"X-Date":           xDate,
		"X-Content-Sha256": contentHash,
		"Content-Type":     arkContentType,
		"Authorization":    authorization,
	}, nil)
}

func ParseArkAgentQuotaResponse(body []byte) (*QuotaResult, error) {
	var payload struct {
		Result struct {
			PlanType    string `json:"PlanType"`
			AFPFiveHour *struct {
				Quota     float64         `json:"Quota"`
				Used      float64         `json:"Used"`
				ResetTime json.RawMessage `json:"ResetTime"`
			} `json:"AFPFiveHour"`
			AFPWeekly *struct {
				Quota     float64         `json:"Quota"`
				Used      float64         `json:"Used"`
				ResetTime json.RawMessage `json:"ResetTime"`
			} `json:"AFPWeekly"`
			AFPMonthly *struct {
				Quota     float64         `json:"Quota"`
				Used      float64         `json:"Used"`
				ResetTime json.RawMessage `json:"ResetTime"`
			} `json:"AFPMonthly"`
		} `json:"Result"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if code, message := arkMetadataError(body); code != "" {
		return nil, fmt.Errorf("方舟 API %s: %s", code, firstNonEmpty(message, code))
	}
	result := &QuotaResult{Level: "Agent Plan " + strings.TrimSpace(payload.Result.PlanType)}
	for name, item := range map[string]*struct {
		Quota     float64         `json:"Quota"`
		Used      float64         `json:"Used"`
		ResetTime json.RawMessage `json:"ResetTime"`
	}{
		"five_hour": payload.Result.AFPFiveHour,
		"weekly":    payload.Result.AFPWeekly,
		"monthly":   payload.Result.AFPMonthly,
	} {
		if item == nil || item.Quota <= 0 {
			continue
		}
		window := QuotaWindow{Name: name, UsedPercent: clamp(item.Used / item.Quota * 100), RemainingPercent: clamp(100 - item.Used/item.Quota*100), UsedValue: item.Used, LimitValue: item.Quota, Unit: "AFP", ResetAt: parseEpochJSON(item.ResetTime)}
		result.Windows = append(result.Windows, window)
	}
	sort.SliceStable(result.Windows, func(i, j int) bool {
		return arkWindowOrder(result.Windows[i].Name) < arkWindowOrder(result.Windows[j].Name)
	})
	return result, nil
}

func ParseArkCodingQuotaResponse(body []byte) (*QuotaResult, error) {
	var payload struct {
		Result struct {
			QuotaUsage []struct {
				Level          string          `json:"Level"`
				Percent        float64         `json:"Percent"`
				UsedPercent    float64         `json:"UsedPercent"`
				UsagePercent   float64         `json:"UsagePercent"`
				ResetTime      json.RawMessage `json:"ResetTime"`
				ResetTimestamp json.RawMessage `json:"ResetTimestamp"`
			} `json:"QuotaUsage"`
			Usages []struct {
				Level          string          `json:"Level"`
				Percent        float64         `json:"Percent"`
				UsedPercent    float64         `json:"UsedPercent"`
				UsagePercent   float64         `json:"UsagePercent"`
				ResetTime      json.RawMessage `json:"ResetTime"`
				ResetTimestamp json.RawMessage `json:"ResetTimestamp"`
			} `json:"Usages"`
			Details []struct {
				Level          string          `json:"Level"`
				Percent        float64         `json:"Percent"`
				UsedPercent    float64         `json:"UsedPercent"`
				UsagePercent   float64         `json:"UsagePercent"`
				ResetTime      json.RawMessage `json:"ResetTime"`
				ResetTimestamp json.RawMessage `json:"ResetTimestamp"`
			} `json:"Details"`
		} `json:"Result"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if code, message := arkMetadataError(body); code != "" {
		return nil, fmt.Errorf("方舟 API %s: %s", code, firstNonEmpty(message, code))
	}
	result := &QuotaResult{Level: "Coding Plan"}
	items := payload.Result.QuotaUsage
	if len(items) == 0 {
		items = payload.Result.Usages
	}
	if len(items) == 0 {
		items = payload.Result.Details
	}
	for _, item := range items {
		name := arkCodingWindow(item.Level)
		if name == "" {
			continue
		}
		percent := item.Percent
		if percent == 0 {
			percent = item.UsedPercent
		}
		if percent == 0 {
			percent = item.UsagePercent
		}
		reset := item.ResetTime
		if len(reset) == 0 {
			reset = item.ResetTimestamp
		}
		result.Windows = append(result.Windows, QuotaWindow{Name: name, UsedPercent: clamp(percent), RemainingPercent: clamp(100 - percent), ResetAt: parseEpochJSON(reset)})
	}
	sort.SliceStable(result.Windows, func(i, j int) bool {
		return arkWindowOrder(result.Windows[i].Name) < arkWindowOrder(result.Windows[j].Name)
	})
	return result, nil
}

func arkWindowOrder(name string) int {
	switch name {
	case "five_hour":
		return 0
	case "weekly":
		return 1
	case "monthly":
		return 2
	default:
		return 3
	}
}

func arkCodingWindow(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "session", "5h", "fivehour", "five_hour", "rolling_5h":
		return "five_hour"
	case "weekly", "week", "7d":
		return "weekly"
	case "monthly", "month":
		return "monthly"
	default:
		return ""
	}
}

func arkRegion(endpoint string) string {
	host := strings.Split(strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://"), "/")[0]
	for _, part := range strings.Split(host, ".") {
		if strings.HasPrefix(part, "cn-") || strings.HasPrefix(part, "ap-") {
			return part
		}
	}
	return arkDefaultRegion
}

func arkCanonicalQuery(action, region string) string {
	values := url.Values{}
	values.Set("Action", action)
	values.Set("Region", region)
	values.Set("Version", arkOpenAPIVersion)
	return values.Encode()
}

func arkSign(accessKey, secretKey, region, query string, body []byte, now time.Time) (string, string, string) {
	xDate := now.UTC().Format("20060102T150405Z")
	shortDate := now.UTC().Format("20060102")
	contentHash := sha256Hex(body)
	canonicalHeaders := "host:" + arkOpenAPIHost + "\nx-date:" + xDate + "\nx-content-sha256:" + contentHash + "\ncontent-type:" + arkContentType + "\n"
	canonicalRequest := "POST\n/\n" + query + "\n" + canonicalHeaders + "\n" + arkSignedHeaders + "\n" + contentHash
	scope := shortDate + "/" + region + "/" + arkService + "/request"
	stringToSign := "HMAC-SHA256\n" + xDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))
	kDate := hmacSHA256([]byte(secretKey), []byte(shortDate))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(arkService))
	kSigning := hmacSHA256(kService, []byte("request"))
	signature := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))
	authorization := "HMAC-SHA256 Credential=" + accessKey + "/" + scope + ", SignedHeaders=" + arkSignedHeaders + ", Signature=" + signature
	return authorization, xDate, contentHash
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func parseEpochJSON(raw json.RawMessage) *time.Time {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var number float64
	if json.Unmarshal(raw, &number) == nil && number > 0 {
		if number >= 1e12 {
			return epochMillis(int64(number))
		}
		return epochSeconds(int64(number))
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if parsed, err := time.Parse(time.RFC3339, text); err == nil {
			return &parsed
		}
		if value, err := strconv.ParseInt(text, 10, 64); err == nil {
			if value >= 1e12 {
				return epochMillis(value)
			}
			return epochSeconds(value)
		}
	}
	return nil
}

func epochMillis(value int64) *time.Time {
	if value <= 0 {
		return nil
	}
	at := time.UnixMilli(value).UTC()
	return &at
}

func epochSeconds(value int64) *time.Time {
	if value <= 0 {
		return nil
	}
	at := time.Unix(value, 0).UTC()
	return &at
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
