package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const (
	grokBillingEndpoint         = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
	maxQuotaResponseBytes       = 1 << 20
	maxHostHTTPRPCResponseBytes = maxQuotaResponseBytes * 2
	maxConcurrentHostHTTPCalls  = 8
)

type hostHTTPGate struct {
	slots chan struct{}
}

type hostHTTPInvoker func([]byte) ([]byte, error)

var sharedHostHTTPGate = newHostHTTPGate(maxConcurrentHostHTTPCalls)

func newHostHTTPGate(maxConcurrent int) *hostHTTPGate {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &hostHTTPGate{slots: make(chan struct{}, maxConcurrent)}
}

func (g *hostHTTPGate) acquire(ctx context.Context) error {
	if g == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case g.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *hostHTTPGate) release() {
	if g == nil {
		return
	}
	<-g.slots
}

func grokBillingRequest(token string) pluginapi.HTTPRequest {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	headers.Set("Accept", "application/json")
	headers.Set("X-XAI-Token-Auth", "xai-grok-cli")
	headers.Set("User-Agent", "CodexBar")
	return pluginapi.HTTPRequest{Method: http.MethodGet, URL: grokBillingEndpoint, Headers: headers}
}

func headerMap(headers http.Header) map[string]string {
	result := make(map[string]string, len(headers))
	for name, values := range headers {
		if len(values) > 0 {
			result[name] = values[0]
		}
	}
	return result
}

func doQuotaHTTP(ctx context.Context, client *Client, request pluginapi.HTTPRequest) ([]byte, int, error) {
	response, err := doHostHTTP(ctx, request)
	if err == nil {
		return response.Body, response.StatusCode, nil
	}
	if client != nil && strings.Contains(err.Error(), "host callback") {
		return client.doRequest(ctx, request.Method, request.URL, headerMap(request.Headers), request.Body)
	}
	return nil, 0, err
}

func codexUsageRequest(token, accountID string) pluginapi.HTTPRequest {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	headers.Set("Accept", "application/json")
	headers.Set("User-Agent", "codex-cli")
	if strings.TrimSpace(accountID) != "" {
		headers.Set("ChatGPT-Account-Id", strings.TrimSpace(accountID))
	}
	return pluginapi.HTTPRequest{Method: http.MethodGet, URL: codexUsageEndpoint, Headers: headers}
}

func geminiLoadRequest(token string) pluginapi.HTTPRequest {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")
	return pluginapi.HTTPRequest{
		Method:  http.MethodPost,
		URL:     geminiCodeAssist,
		Headers: headers,
		Body:    []byte(`{"metadata":{"ideType":"GEMINI_CLI","pluginType":"GEMINI"}}`),
	}
}

func geminiQuotaRequest(token, projectID string) pluginapi.HTTPRequest {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")
	body := []byte(`{}`)
	if strings.TrimSpace(projectID) != "" {
		body = []byte(`{"project":` + jsonString(projectID) + `}`)
	}
	return pluginapi.HTTPRequest{Method: http.MethodPost, URL: geminiQuotaEndpoint, Headers: headers, Body: body}
}

func antigravitySummaryRequest(token, projectID, url string) pluginapi.HTTPRequest {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")
	headers.Set("User-Agent", antigravityUserAgent)
	body := []byte(`{"project":` + jsonString(firstNonEmpty(projectID, antigravityDefaultProjectID)) + `}`)
	return pluginapi.HTTPRequest{Method: http.MethodPost, URL: url, Headers: headers, Body: body}
}

func jsonString(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func doHostHTTP(ctx context.Context, request pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	return doHostHTTPWithInvoker(ctx, request, sharedHostHTTPGate, func(rawRequest []byte) ([]byte, error) {
		return callHostWithResponseLimit(pluginabi.MethodHostHTTPDo, rawRequest, maxHostHTTPRPCResponseBytes)
	})
}

func doHostHTTPWithInvoker(ctx context.Context, request pluginapi.HTTPRequest, gate *hostHTTPGate, invoke hostHTTPInvoker) (pluginapi.HTTPResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if invoke == nil {
		return pluginapi.HTTPResponse{}, fmt.Errorf("host HTTP invoker is unavailable")
	}
	rawRequest, err := json.Marshal(request)
	if err != nil {
		return pluginapi.HTTPResponse{}, fmt.Errorf("encode host HTTP request: %w", err)
	}
	if err := gate.acquire(ctx); err != nil {
		return pluginapi.HTTPResponse{}, err
	}
	type outcome struct {
		response pluginapi.HTTPResponse
		err      error
	}
	result := make(chan outcome, 1)
	go func() {
		defer gate.release()
		if err := ctx.Err(); err != nil {
			result <- outcome{err: err}
			return
		}
		rawResponse, errCall := invoke(rawRequest)
		if errCall != nil {
			result <- outcome{err: errCall}
			return
		}
		var response pluginapi.HTTPResponse
		if errDecode := decodeRPCResult(rawResponse, &response); errDecode != nil {
			result <- outcome{err: fmt.Errorf("decode host HTTP response: %w", errDecode)}
			return
		}
		if len(response.Body) > maxQuotaResponseBytes {
			result <- outcome{err: fmt.Errorf("host HTTP response body exceeds %d bytes", maxQuotaResponseBytes)}
			return
		}
		result <- outcome{response: response}
	}()
	select {
	case out := <-result:
		return out.response, out.err
	case <-ctx.Done():
		return pluginapi.HTTPResponse{}, ctx.Err()
	}
}

func grokBillingHeaderMap(token string) map[string]string {
	request := grokBillingRequest(token)
	result := make(map[string]string, len(request.Headers))
	for name, values := range request.Headers {
		if len(values) > 0 {
			result[name] = values[0]
		}
	}
	return result
}

func ParseGrokBillingResponse(body []byte, now time.Time) (*QuotaResult, error) {
	_ = now
	var payload struct {
		Config *struct {
			CreditUsagePercent *float64 `json:"creditUsagePercent"`
			CurrentPeriod      *struct {
				End string `json:"end"`
			} `json:"currentPeriod"`
			BillingPeriodEnd string `json:"billingPeriodEnd"`
			OnDemandCap      *struct {
				Value *float64 `json:"val"`
			} `json:"onDemandCap"`
			OnDemandUsed *struct {
				Value *float64 `json:"val"`
			} `json:"onDemandUsed"`
		} `json:"config"`
		CreditUsagePercent *float64 `json:"creditUsagePercent"`
		CreditResetAt      string   `json:"creditResetAt"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var usage *float64
	resetText := strings.TrimSpace(payload.CreditResetAt)
	if payload.Config != nil {
		usage = payload.Config.CreditUsagePercent
		if payload.Config.CurrentPeriod != nil {
			resetText = strings.TrimSpace(payload.Config.CurrentPeriod.End)
		}
		if resetText == "" {
			resetText = strings.TrimSpace(payload.Config.BillingPeriodEnd)
		}
		if usage == nil && payload.Config.OnDemandCap != nil && payload.Config.OnDemandUsed != nil &&
			payload.Config.OnDemandCap.Value != nil && payload.Config.OnDemandUsed.Value != nil && *payload.Config.OnDemandCap.Value > 0 {
			value := *payload.Config.OnDemandUsed.Value / *payload.Config.OnDemandCap.Value * 100
			usage = &value
		}
	}
	if usage == nil {
		usage = payload.CreditUsagePercent
	}
	if usage == nil && resetText == "" {
		return nil, fmt.Errorf("creditUsagePercent is missing")
	}
	if usage == nil {
		value := 0.0
		usage = &value
	}
	window := QuotaWindow{
		Name:             "credits",
		UsedPercent:      clamp(*usage),
		RemainingPercent: clamp(100 - *usage),
	}
	if resetText != "" {
		if reset, err := parseGrokBillingTime(resetText); err == nil {
			window.ResetAt = reset
		}
	}
	return &QuotaResult{Level: "Grok", Windows: []QuotaWindow{window}}, nil
}

func parseGrokBillingTime(value string) (*time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("invalid Grok billing reset time")
}
