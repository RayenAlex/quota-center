package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type AuthStore interface {
	List(context.Context) ([]StoredCredential, error)
	Get(context.Context, string) (StoredCredential, error)
	Save(context.Context, string, []byte) (StoredCredential, error)
}

type StoredCredential struct {
	AuthIndex string
	Name      string
	Provider  Provider
	Type      string
	Label     string
	Disabled  bool
	JSON      json.RawMessage
	Path      string
}

type hostAuthStore struct{}

func (hostAuthStore) List(_ context.Context) ([]StoredCredential, error) {
	raw, err := callHost("host.auth.list", nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Files []struct {
			AuthIndex string `json:"auth_index"`
			Name      string `json:"name"`
			Type      string `json:"type"`
			Provider  string `json:"provider"`
			Label     string `json:"label"`
			Status    string `json:"status"`
			Disabled  bool   `json:"disabled"`
			Path      string `json:"path"`
		} `json:"files"`
	}
	if err := decodeRPCResult(raw, &response); err != nil {
		return nil, fmt.Errorf("decode host auth list: %w", err)
	}
	entries := make([]StoredCredential, 0, len(response.Files))
	for _, file := range response.Files {
		provider := hostProviderAliases(file.Type, file.Provider)
		if _, ok := supportedProviders[provider]; !ok {
			continue
		}
		entries = append(entries, StoredCredential{
			AuthIndex: file.AuthIndex,
			Name:      file.Name,
			Provider:  provider,
			Type:      firstNonEmpty(file.Type, file.Provider),
			Label:     firstNonEmpty(file.Label, file.Name),
			Disabled:  file.Disabled || strings.EqualFold(file.Status, "disabled"),
			Path:      file.Path,
		})
	}
	return entries, nil
}

func (hostAuthStore) Get(_ context.Context, authIndex string) (StoredCredential, error) {
	request, _ := json.Marshal(map[string]string{"auth_index": authIndex})
	raw, err := callHost("host.auth.get", request)
	if err != nil {
		return StoredCredential{}, err
	}
	var response struct {
		AuthIndex string          `json:"auth_index"`
		Name      string          `json:"name"`
		Type      string          `json:"type"`
		Provider  string          `json:"provider"`
		Label     string          `json:"label"`
		Status    string          `json:"status"`
		Disabled  bool            `json:"disabled"`
		Path      string          `json:"path"`
		JSON      json.RawMessage `json:"json"`
	}
	if err := decodeRPCResult(raw, &response); err != nil {
		return StoredCredential{}, fmt.Errorf("decode host auth get: %w", err)
	}
	return StoredCredential{
		AuthIndex: response.AuthIndex,
		Name:      response.Name,
		Provider:  hostProviderAliases(response.Type, response.Provider),
		Type:      firstNonEmpty(response.Type, response.Provider),
		Label:     response.Label,
		Disabled:  response.Disabled || strings.EqualFold(response.Status, "disabled"),
		JSON:      response.JSON,
		Path:      response.Path,
	}, nil
}

func (hostAuthStore) Save(_ context.Context, name string, credential []byte) (StoredCredential, error) {
	request, _ := json.Marshal(map[string]any{"name": name, "json": json.RawMessage(credential)})
	raw, err := callHost("host.auth.save", request)
	if err != nil {
		return StoredCredential{}, err
	}
	var response struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := decodeRPCResult(raw, &response); err != nil {
		return StoredCredential{}, fmt.Errorf("decode host auth save: %w", err)
	}
	return StoredCredential{Name: response.Name, Path: response.Path, JSON: credential}, nil
}

func accountFromStoredCredential(entry StoredCredential) (PlanConfig, error) {
	var payload struct {
		ID          string `json:"id"`
		Provider    string `json:"provider"`
		Type        string `json:"type"`
		Plan        string `json:"plan"`
		Label       string `json:"label"`
		Email       string `json:"email"`
		APIKey      string `json:"api_key"`
		AccessToken string `json:"access_token"`
		AccountID   string `json:"account_id"`
		ProjectID   string `json:"project_id"`
		Token       struct {
			AccessToken string `json:"access_token"`
		} `json:"token"`
		AccessKey string `json:"access_key_id"`
		SecretKey string `json:"secret_access_key"`
		Endpoint  string `json:"endpoint"`
	}
	if len(entry.JSON) > 0 {
		if err := json.Unmarshal(entry.JSON, &payload); err != nil {
			return PlanConfig{}, fmt.Errorf("decode saved credential: %w", err)
		}
	}
	provider := firstNonEmpty(
		hostProviderAliases(payload.Type, payload.Provider),
		hostProviderAliases("", string(entry.Provider)),
	)
	if _, ok := supportedProviders[provider]; !ok {
		return PlanConfig{}, fmt.Errorf("unsupported saved provider %q", provider)
	}
	if payload.Endpoint != "" {
		if err := validateProviderEndpoint(provider, payload.Endpoint); err != nil {
			return PlanConfig{}, fmt.Errorf("saved endpoint: %w", err)
		}
	}
	accessToken := firstNonEmpty(payload.AccessToken, payload.Token.AccessToken)
	source := AuthSourceConfig
	if isCPANativeCredential(provider, payload.ID, firstNonEmpty(payload.AccountID, payload.ProjectID), firstNonEmpty(payload.Token.AccessToken, accessToken), payload.Type, string(entry.Provider)) {
		source = AuthSourceCPA
	}
	label := firstNonEmpty(payload.Label, payload.Email, entry.Label, entry.Name)
	id := firstNonEmpty(payload.ID, entry.AuthIndex, entry.Name)
	plan := PlanConfig{
		ID:          id,
		Label:       label,
		Source:      source,
		AuthType:    firstNonEmpty(payload.Type, entry.Type, string(entry.Provider)),
		Provider:    provider,
		Plan:        nativePlanForSource(source, provider, payload.Plan),
		APIKey:      payload.APIKey,
		AccessToken: accessToken,
		AccountID:   firstNonEmpty(payload.AccountID, payload.ProjectID),
		AccessKey:   payload.AccessKey,
		SecretKey:   payload.SecretKey,
		Endpoint:    payload.Endpoint,
		AuthIndex:   entry.AuthIndex,
		Disabled:    entry.Disabled,
	}
	if strings.Contains(strings.ToLower(plan.AuthType), "antigravity") && strings.EqualFold(plan.Plan, "oauth") {
		plan.Plan = ""
	}
	return plan, nil
}

func hostProviderAliases(providerType, provider string) Provider {
	normalizedType := strings.ToLower(strings.TrimSpace(providerType))
	switch normalizedType {
	case "gemini-cli", "antigravity", "gemini":
		return ProviderGemini
	case "xai":
		return ProviderGrok
	case "codex":
		return ProviderCodex
	}
	if normalizedType != "" {
		return Provider(normalizedType)
	}
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	switch normalizedProvider {
	case "xai":
		return ProviderGrok
	case "gemini-cli", "antigravity", "gemini":
		return ProviderGemini
	}
	return Provider(normalizedProvider)
}

func isCPANativeCredential(provider Provider, payloadID, accountID, nestedAccessToken, providerType, entryProvider string) bool {
	if payloadID != "" {
		return false
	}
	switch provider {
	case ProviderCodex:
		return accountID != "" || strings.EqualFold(entryProvider, ProviderCodex)
	case ProviderGemini:
		return isGeminiNativeType(providerType) || isGeminiNativeType(entryProvider) || nestedAccessToken != ""
	case ProviderGrok:
		return strings.EqualFold(providerType, "xai") || strings.EqualFold(entryProvider, "xai")
	default:
		return false
	}
}

func isGeminiNativeType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "gemini", "gemini-cli", "antigravity":
		return true
	default:
		return false
	}
}

func nativePlanForSource(source AuthSource, provider Provider, configured string) string {
	if configured != "" {
		return configured
	}
	if source == AuthSourceCPA {
		switch provider {
		case ProviderCodex, ProviderGemini, ProviderGrok:
			return "oauth"
		}
	}
	return defaultPlanForProvider(provider)
}
