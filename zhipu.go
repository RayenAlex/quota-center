package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type QuotaClient interface {
	FetchQuota(context.Context, PlanConfig) (*QuotaResult, error)
}

type QuotaHTTPClientFunc func(context.Context, *http.Request) (*http.Response, error)

func (f QuotaHTTPClientFunc) FetchQuota(ctx context.Context, plan PlanConfig) (*QuotaResult, error) {
	return NewClient(f).FetchQuota(ctx, plan)
}

type Client struct {
	endpoint string
	httpDo   QuotaHTTPClientFunc
}

func NewClient(httpDo QuotaHTTPClientFunc) *Client {
	return &Client{endpoint: defaultQuotaEndpoint, httpDo: httpDo}
}

func (c *Client) SetEndpoint(endpoint string) {
	if endpoint != "" {
		c.endpoint = endpoint
	}
}

func (c *Client) FetchQuota(ctx context.Context, plan PlanConfig) (*QuotaResult, error) {
	switch plan.Provider {
	case ProviderMiniMax:
		return c.fetchMiniMax(ctx, plan)
	case ProviderArk:
		return c.fetchArk(ctx, plan)
	case ProviderCodex:
		return c.fetchCodex(ctx, plan)
	case ProviderGemini:
		return c.fetchGemini(ctx, plan)
	case ProviderGrok:
		return c.fetchGrok(ctx, plan)
	case ProviderOpenCode:
		return nil, fmt.Errorf("%s does not expose a stable public quota endpoint", providerLabel(plan.Provider))
	default:
		return c.fetchZhipu(ctx, plan)
	}
}

func (c *Client) fetchZhipu(ctx context.Context, plan PlanConfig) (*QuotaResult, error) {
	endpoint := c.endpoint
	if plan.Endpoint != "" {
		endpoint = plan.Endpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", plan.APIKey)
	req.Header.Set("Accept", "application/json")
	response, err := c.httpDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("request quota: %w", err)
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return nil, fmt.Errorf("quota endpoint returned a redirect")
	}
	defer response.Body.Close()
	body, err := readQuotaResponse(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read quota response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Zhipu API %d: %s", response.StatusCode, sanitizeMessage(string(body)))
	}
	result, err := ParseQuotaResponse(body)
	if err != nil {
		return nil, fmt.Errorf("decode quota response: %w", err)
	}
	return result, nil
}

type quotaResponse struct {
	Data struct {
		Level  string       `json:"level"`
		Limits []quotaLimit `json:"limits"`
	} `json:"data"`
}

type quotaLimit struct {
	Type          string  `json:"type"`
	Unit          int     `json:"unit"`
	Percentage    float64 `json:"percentage"`
	NextResetTime float64 `json:"nextResetTime"`
}

type QuotaResult struct {
	Level    string        `json:"level"`
	FiveHour QuotaWindow   `json:"five_hour"`
	Weekly   QuotaWindow   `json:"weekly"`
	MCP      QuotaWindow   `json:"mcp"`
	Monthly  QuotaWindow   `json:"monthly"`
	Windows  []QuotaWindow `json:"windows,omitempty"`
}

type QuotaWindow struct {
	Name             string     `json:"name,omitempty"`
	Group            string     `json:"group,omitempty"`
	UsedPercent      float64    `json:"used_percent"`
	RemainingPercent float64    `json:"remaining_percent"`
	Available        bool       `json:"available,omitempty"`
	ResetAt          *time.Time `json:"reset_at,omitempty"`
	UsedValue        float64    `json:"used_value,omitempty"`
	LimitValue       float64    `json:"limit_value,omitempty"`
	Unit             string     `json:"unit,omitempty"`
}

func ParseQuotaResponse(body []byte) (*QuotaResult, error) {
	var payload quotaResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.Data.Limits == nil {
		return nil, fmt.Errorf("data.limits must be an array")
	}
	result := &QuotaResult{Level: strings.TrimSpace(payload.Data.Level)}
	for _, limit := range payload.Data.Limits {
		used := clamp(limit.Percentage)
		window := QuotaWindow{
			Name:             zhipuWindowName(limit),
			UsedPercent:      used,
			RemainingPercent: clamp(100 - used),
		}
		if reset := resetTime(limit.NextResetTime); reset != nil {
			window.ResetAt = reset
		}
		switch {
		case limit.Type == "TOKENS_LIMIT" && limit.Unit == 3:
			result.FiveHour = window
			result.Windows = append(result.Windows, window)
		case limit.Type == "TOKENS_LIMIT" && limit.Unit == 6:
			result.Weekly = window
			result.Windows = append(result.Windows, window)
		case limit.Type == "TIME_LIMIT":
			result.MCP = window
			result.Windows = append(result.Windows, window)
		}
	}
	return result, nil
}

func zhipuWindowName(limit quotaLimit) string {
	switch {
	case limit.Type == "TOKENS_LIMIT" && limit.Unit == 3:
		return "five_hour"
	case limit.Type == "TOKENS_LIMIT" && limit.Unit == 6:
		return "weekly"
	case limit.Type == "TIME_LIMIT":
		return "mcp"
	default:
		return ""
	}
}

func resetTime(value float64) *time.Time {
	if value <= 0 {
		return nil
	}
	at := time.UnixMilli(int64(value)).UTC()
	return &at
}

func clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func sanitizeMessage(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 200 {
		message = message[:200]
	}
	return message
}

func readQuotaResponse(body io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxQuotaResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxQuotaResponseBytes {
		return nil, fmt.Errorf("quota response body exceeds %d bytes", maxQuotaResponseBytes)
	}
	return raw, nil
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
