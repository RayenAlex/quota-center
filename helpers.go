package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func getEnvironment(name string) string { return os.Getenv(name) }

func contextWithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return context.WithTimeout(parent, timeout)
}

func nowUTC(now func() time.Time) time.Time {
	if now == nil {
		now = time.Now
	}
	return now().UTC()
}

func okEnvelope(value any) []byte {
	raw, _ := json.Marshal(envelope{OK: true, Result: mustJSON(value)})
	return raw
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

func mustJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func jsonManagementResponse(status int, value any) pluginapi.ManagementResponse {
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json; charset=utf-8")
	return pluginapi.ManagementResponse{StatusCode: status, Headers: headers, Body: mustJSON(value)}
}

func htmlManagementResponse(body string) pluginapi.ManagementResponse {
	headers := make(http.Header)
	headers.Set("Content-Type", "text/html; charset=utf-8")
	return pluginapi.ManagementResponse{StatusCode: http.StatusOK, Headers: headers, Body: []byte(body)}
}
