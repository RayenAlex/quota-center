package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseQuotaResponseMapsWindows(t *testing.T) {
	reset := time.UnixMilli(1735776000000).UTC()
	result, err := ParseQuotaResponse([]byte(`{
		"code": 0,
		"msg": "ok",
		"success": true,
		"data": {
			"level": "pro",
			"limits": [
				{"type":"TOKENS_LIMIT","unit":3,"percentage":25,"nextResetTime":1735776000000},
				{"type":"TOKENS_LIMIT","unit":6,"percentage":70,"nextResetTime":1735776000000},
				{"type":"TIME_LIMIT","unit":9,"percentage":10,"nextResetTime":-1},
				{"type":"UNKNOWN_LIMIT","unit":4,"percentage":99,"nextResetTime":1}
			]
		}
	}`))
	if err != nil {
		t.Fatalf("ParseQuotaResponse() error = %v", err)
	}
	if result.Level != "pro" {
		t.Fatalf("Level = %q, want pro", result.Level)
	}
	if result.FiveHour.RemainingPercent != 75 || result.FiveHour.UsedPercent != 25 {
		t.Fatalf("FiveHour = %#v", result.FiveHour)
	}
	if !result.FiveHour.ResetAt.Equal(reset) {
		t.Fatalf("FiveHour.ResetAt = %s, want %s", result.FiveHour.ResetAt, reset)
	}
	if result.Weekly.RemainingPercent != 30 || result.Weekly.UsedPercent != 70 {
		t.Fatalf("Weekly = %#v", result.Weekly)
	}
	if result.MCP.RemainingPercent != 90 || result.MCP.ResetAt != nil {
		t.Fatalf("MCP = %#v", result.MCP)
	}
}

func TestParseQuotaResponseClampsAndKeepsLastDuplicate(t *testing.T) {
	result, err := ParseQuotaResponse([]byte(`{
		"data":{"limits":[
			{"type":"TOKENS_LIMIT","unit":3,"percentage":130},
			{"type":"TOKENS_LIMIT","unit":3,"percentage":-20}
		]}
	}`))
	if err != nil {
		t.Fatalf("ParseQuotaResponse() error = %v", err)
	}
	if result.FiveHour.RemainingPercent != 100 || result.FiveHour.UsedPercent != 0 {
		t.Fatalf("FiveHour = %#v", result.FiveHour)
	}
}

func TestParseQuotaResponseRejectsInvalidPayload(t *testing.T) {
	tests := []string{
		`{}`,
		`{"data":{}}`,
		`{"data":{"limits":null}}`,
		`{"data":{"limits":{}}}`,
		`not-json`,
	}
	for _, body := range tests {
		if _, err := ParseQuotaResponse([]byte(body)); err == nil {
			t.Fatalf("ParseQuotaResponse(%q) error = nil, want error", body)
		}
	}
}

func TestClientFetchQuotaContract(t *testing.T) {
	var seenAuth string
	var seenAccept string
	client := NewClient(QuotaHTTPClientFunc(func(ctx context.Context, req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://quota.example.test/limit" {
			t.Fatalf("URL = %s", req.URL)
		}
		if req.Method != http.MethodGet {
			t.Fatalf("Method = %s", req.Method)
		}
		seenAuth = req.Header.Get("Authorization")
		seenAccept = req.Header.Get("Accept")
		return jsonResponse(http.StatusOK, `{"data":{"limits":[]}}`), nil
	}))
	client.SetEndpoint("https://quota.example.test/limit")
	result, err := client.FetchQuota(context.Background(), PlanConfig{ID: "x", Label: "x", APIKey: "secret-key"})
	if err != nil {
		t.Fatalf("FetchQuota() error = %v", err)
	}
	if seenAuth != "secret-key" {
		t.Fatalf("Authorization = %q", seenAuth)
	}
	if seenAccept != "application/json" {
		t.Fatalf("Accept = %q", seenAccept)
	}
	if result.Level != "" {
		t.Fatalf("Level = %q", result.Level)
	}
}

func TestClientFetchQuotaErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "http error", status: http.StatusUnauthorized, body: `{"msg":"invalid key"}`, want: "Zhipu API 401"},
		{name: "invalid json", status: http.StatusOK, body: "nope", want: "decode quota response"},
		{name: "transport", want: "network unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
				if tt.name == "transport" {
					return nil, errors.New("network unavailable")
				}
				return jsonResponse(tt.status, tt.body), nil
			}))
			_, err := client.FetchQuota(context.Background(), PlanConfig{ID: "x", APIKey: "key"})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("FetchQuota() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestClientFetchQuotaCancels(t *testing.T) {
	client := NewClient(QuotaHTTPClientFunc(func(ctx context.Context, req *http.Request) (*http.Response, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.FetchQuota(ctx, PlanConfig{ID: "x", APIKey: "key"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchQuota() error = %v, want context canceled", err)
	}
}
