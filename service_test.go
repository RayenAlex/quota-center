package main

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestRefreshAllReturnsIndependentPlanResults(t *testing.T) {
	var mu sync.Mutex
	inFlight := 0
	maxInFlight := 0
	client := NewClient(QuotaHTTPClientFunc(func(ctx context.Context, req *http.Request) (*http.Response, error) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()
		select {
		case <-time.After(30 * time.Millisecond):
		case <-ctx.Done():
			mu.Lock()
			inFlight--
			mu.Unlock()
			return nil, ctx.Err()
		}
		mu.Lock()
		inFlight--
		mu.Unlock()
		if req.Header.Get("Authorization") == "pro-key" {
			return jsonResponse(http.StatusInternalServerError, `{"msg":"boom"}`), nil
		}
		return jsonResponse(http.StatusOK, `{"data":{"level":"max","limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":20}]}}`), nil
	}))
	service := NewService(Config{Timeout: time.Second, Endpoint: "https://quota.example.test"}, client)
	results := service.RefreshAll(context.Background(), []PlanConfig{
		{ID: "pro", Label: "Pro", APIKey: "pro-key"},
		{ID: "max", Label: "Max", APIKey: "max-key"},
	})
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if maxInFlight != 2 {
		t.Fatalf("max in-flight = %d, want 2", maxInFlight)
	}
	if results[0].ID != "pro" || results[0].Error == "" || results[0].Quota != nil {
		t.Fatalf("results[0] = %#v", results[0])
	}
	if results[1].ID != "max" || results[1].Error != "" || results[1].Quota == nil || results[1].Quota.Level != "max" {
		t.Fatalf("results[1] = %#v", results[1])
	}
	if results[0].FetchedAt.IsZero() || results[1].FetchedAt.IsZero() {
		t.Fatal("FetchedAt must be set for success and failure")
	}
}
