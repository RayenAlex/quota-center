package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseGrokBillingResponse(t *testing.T) {
	now := time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC)
	result, err := ParseGrokBillingResponse([]byte(`{"config":{"creditUsagePercent":37.5,"currentPeriod":{"end":"2026-08-16T00:00:00Z"}}}`), now)
	if err != nil {
		t.Fatalf("ParseGrokBillingResponse() error = %v", err)
	}
	if result.Level != "Grok" || len(result.Windows) != 1 {
		t.Fatalf("result = %#v", result)
	}
	window := result.Windows[0]
	if window.Name != "credits" || window.UsedPercent != 37.5 || window.RemainingPercent != 62.5 {
		t.Fatalf("window = %#v", window)
	}
	if window.ResetAt == nil || !window.ResetAt.Equal(time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("reset = %v", window.ResetAt)
	}
}

func TestParseGrokBillingResponseRejectsMissingUsage(t *testing.T) {
	_, err := ParseGrokBillingResponse([]byte(`{"config":{}}`), time.Now())
	if err == nil || !strings.Contains(err.Error(), "creditUsagePercent") {
		t.Fatalf("error = %v, want missing creditUsagePercent", err)
	}
}

func TestGrokBillingRequestUsesCPAHostTransport(t *testing.T) {
	request := grokBillingRequest("test-token")
	if request.Method != "GET" || request.URL != grokBillingEndpoint {
		t.Fatalf("request = %#v", request)
	}
	for _, header := range []string{"Authorization", "X-XAI-Token-Auth", "User-Agent", "Accept"} {
		if request.Headers.Get(header) == "" {
			t.Fatalf("missing header %q: %#v", header, request.Headers)
		}
	}
	if request.Headers.Get("X-XAI-Token-Auth") != "xai-grok-cli" {
		t.Fatalf("X-XAI-Token-Auth = %q", request.Headers.Get("X-XAI-Token-Auth"))
	}
	if request.Headers.Get("User-Agent") != "CodexBar" {
		t.Fatalf("User-Agent = %q", request.Headers.Get("User-Agent"))
	}
}

func TestCodexUsageRequestUsesCPAHostTransport(t *testing.T) {
	request := codexUsageRequest("test-token", "acct-1")
	if request.Method != "GET" || request.URL != codexUsageEndpoint {
		t.Fatalf("request = %#v", request)
	}
	if request.Headers.Get("Authorization") != "Bearer test-token" || request.Headers.Get("ChatGPT-Account-Id") != "acct-1" {
		t.Fatalf("headers = %#v", request.Headers)
	}
}

func TestGeminiQuotaRequestUsesCPAHostTransport(t *testing.T) {
	request := geminiQuotaRequest("test-token", "proj-1")
	if request.Method != "POST" || request.URL != geminiQuotaEndpoint {
		t.Fatalf("request = %#v", request)
	}
	if request.Headers.Get("Authorization") != "Bearer test-token" || !strings.Contains(string(request.Body), "proj-1") {
		t.Fatalf("request = %#v body=%s", request, request.Body)
	}
}
