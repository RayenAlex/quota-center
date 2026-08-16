package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestReadQuotaResponseRejectsBodyLargerThanLimit(t *testing.T) {
	_, err := readQuotaResponse(bytes.NewReader(bytes.Repeat([]byte("x"), maxQuotaResponseBytes+1)))
	if err == nil || !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("readQuotaResponse() error = %v, want response body limit", err)
	}
}

func TestDoHostHTTPWithInvokerRejectsOversizedResponseBody(t *testing.T) {
	response := pluginapi.HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       bytes.Repeat([]byte("x"), maxQuotaResponseBytes+1),
	}

	_, err := doHostHTTPWithInvoker(
		context.Background(),
		pluginapi.HTTPRequest{Method: http.MethodGet, URL: "https://example.test/quota"},
		newHostHTTPGate(1),
		func([]byte) ([]byte, error) { return okEnvelope(response), nil },
	)
	if err == nil || !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("doHostHTTPWithInvoker() error = %v, want response body limit", err)
	}
}

func TestDoHostHTTPWithInvokerBoundsTimedOutCallbacks(t *testing.T) {
	gate := newHostHTTPGate(1)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	invoker := func([]byte) ([]byte, error) {
		if calls.Add(1) == 1 {
			started <- struct{}{}
			<-release
		}
		return okEnvelope(pluginapi.HTTPResponse{StatusCode: http.StatusNoContent}), nil
	}

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := doHostHTTPWithInvoker(firstCtx, pluginapi.HTTPRequest{Method: http.MethodGet, URL: "https://example.test/quota"}, gate, invoker)
		firstDone <- err
	}()
	<-started
	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first request error = %v, want context cancellation", err)
	}

	secondCtx, cancelSecond := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() {
		_, err := doHostHTTPWithInvoker(secondCtx, pluginapi.HTTPRequest{Method: http.MethodGet, URL: "https://example.test/quota"}, gate, invoker)
		secondDone <- err
	}()
	cancelSecond()
	if err := <-secondDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("second request error = %v, want context cancellation", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("callback invocations = %d, want one while the timed-out callback is still running", got)
	}

	close(release)
	thirdCtx, cancelThird := context.WithTimeout(context.Background(), time.Second)
	defer cancelThird()
	if _, err := doHostHTTPWithInvoker(thirdCtx, pluginapi.HTTPRequest{Method: http.MethodGet, URL: "https://example.test/quota"}, gate, invoker); err != nil {
		t.Fatalf("callback slot was not released after completion: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("callback invocations = %d, want two after releasing the first callback", got)
	}
}
