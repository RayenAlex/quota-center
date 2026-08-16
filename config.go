package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultQuotaEndpoint = "https://bigmodel.cn/api/monitor/usage/quota/limit"

var allowedEndpointHosts = map[Provider][]string{
	ProviderZhipu:   {"bigmodel.cn", "open.bigmodel.cn"},
	ProviderMiniMax: {"api.minimaxi.com", "api.minimax.io", "api.minimax.chat"},
}

var planIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Config struct {
	Enabled  bool
	Priority int
	Timeout  time.Duration
	Endpoint string
	Plans    []PlanConfig
}

type Provider = string

type AuthSource = string

const (
	ProviderZhipu    Provider = "zhipu"
	ProviderMiniMax  Provider = "minimax"
	ProviderOpenCode Provider = "opencode-go"
	ProviderArk      Provider = "ark"
	ProviderGrok     Provider = "grok"
	ProviderCodex    Provider = "codex"
	ProviderGemini   Provider = "gemini"
)

const (
	AuthSourceConfig AuthSource = "config"
	AuthSourceCPA    AuthSource = "cpa"
)

var supportedProviders = map[Provider]struct{}{
	ProviderZhipu:    {},
	ProviderMiniMax:  {},
	ProviderOpenCode: {},
	ProviderArk:      {},
	ProviderGrok:     {},
	ProviderCodex:    {},
	ProviderGemini:   {},
}

type PlanConfig struct {
	ID             string
	Label          string
	Source         AuthSource
	AuthType       string
	APIKeyEnv      string
	APIKey         string
	Provider       Provider
	Plan           string
	Endpoint       string
	AuthIndex      string
	AccessTokenEnv string
	AccessToken    string
	AccountID      string
	Disabled       bool
	AccessKeyEnv   string
	AccessKey      string
	SecretKeyEnv   string
	SecretKey      string
}

type rawAccountConfig struct {
	ID             string `yaml:"id"`
	Label          string `yaml:"label"`
	Provider       string `yaml:"provider"`
	Plan           string `yaml:"plan"`
	APIKeyEnv      string `yaml:"api_key_env"`
	AccessTokenEnv string `yaml:"access_token_env"`
	AccessKeyEnv   string `yaml:"access_key_env"`
	SecretKeyEnv   string `yaml:"secret_key_env"`
	Endpoint       string `yaml:"endpoint"`
	AuthIndex      string `yaml:"auth_index"`
}

type rawConfig struct {
	Enabled  *bool              `yaml:"enabled"`
	Priority *int               `yaml:"priority"`
	Timeout  string             `yaml:"timeout"`
	Endpoint string             `yaml:"endpoint"`
	Accounts []rawAccountConfig `yaml:"accounts"`
	Plans    []rawAccountConfig `yaml:"plans"`
}

func DecodeConfig(raw []byte, getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	decoded := rawConfig{
		Enabled:  new(bool),
		Priority: new(int),
		Timeout:  "15s",
		Endpoint: defaultQuotaEndpoint,
	}
	*decoded.Enabled = true
	*decoded.Priority = 1
	if len(raw) != 0 {
		if err := yaml.Unmarshal(raw, &decoded); err != nil {
			return Config{}, fmt.Errorf("decode config: %w", err)
		}
	}

	cfg := Config{
		Enabled:  *decoded.Enabled,
		Priority: *decoded.Priority,
		Timeout:  15 * time.Second,
		Endpoint: strings.TrimSpace(decoded.Endpoint),
	}
	if decoded.Timeout != "" {
		timeout, err := time.ParseDuration(decoded.Timeout)
		if err != nil || timeout <= 0 {
			return Config{}, fmt.Errorf("timeout must be a positive duration")
		}
		cfg.Timeout = timeout
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = defaultQuotaEndpoint
	}
	if err := validateProviderEndpoint(ProviderZhipu, cfg.Endpoint); err != nil {
		return Config{}, fmt.Errorf("endpoint: %w", err)
	}

	type accountEntry struct {
		source string
		index  int
		value  rawAccountConfig
	}
	entries := make([]accountEntry, 0, len(decoded.Accounts)+len(decoded.Plans))
	for index, account := range decoded.Accounts {
		entries = append(entries, accountEntry{source: "accounts", index: index, value: account})
	}
	for index, plan := range decoded.Plans {
		entries = append(entries, accountEntry{source: "plans", index: index, value: plan})
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		index, plan := entry.index, entry.value
		fieldPrefix := entry.source
		id := strings.TrimSpace(plan.ID)
		if id == "" {
			return Config{}, fmt.Errorf("%s[%d].id is required", fieldPrefix, index)
		}
		if !planIDPattern.MatchString(id) {
			return Config{}, fmt.Errorf("%s[%d].id must contain only ASCII letters, digits, dot, underscore, or dash", fieldPrefix, index)
		}
		if _, exists := seen[id]; exists {
			return Config{}, fmt.Errorf("duplicate plan id %q", id)
		}
		seen[id] = struct{}{}
		provider := Provider(strings.ToLower(strings.TrimSpace(plan.Provider)))
		if provider == "" {
			provider = ProviderZhipu
		}
		if _, ok := supportedProviders[provider]; !ok {
			return Config{}, fmt.Errorf("%s[%d].provider %q is unsupported", fieldPrefix, index, provider)
		}
		label := strings.TrimSpace(plan.Label)
		if label == "" {
			label = id
		}
		account := PlanConfig{
			ID:             id,
			Label:          label,
			Source:         AuthSourceConfig,
			Provider:       provider,
			Plan:           strings.TrimSpace(plan.Plan),
			Endpoint:       strings.TrimSpace(plan.Endpoint),
			AuthIndex:      strings.TrimSpace(plan.AuthIndex),
			APIKeyEnv:      strings.TrimSpace(plan.APIKeyEnv),
			AccessTokenEnv: strings.TrimSpace(plan.AccessTokenEnv),
			AccessKeyEnv:   strings.TrimSpace(plan.AccessKeyEnv),
			SecretKeyEnv:   strings.TrimSpace(plan.SecretKeyEnv),
		}
		if account.Plan == "" {
			account.Plan = defaultPlanForProvider(provider)
		}
		if account.Endpoint != "" {
			if err := validateProviderEndpoint(provider, account.Endpoint); err != nil {
				return Config{}, fmt.Errorf("%s[%d].endpoint: %w", fieldPrefix, index, err)
			}
		}
		if account.APIKeyEnv != "" {
			account.APIKey = strings.TrimSpace(getenv(account.APIKeyEnv))
		}
		if account.AccessTokenEnv != "" {
			account.AccessToken = strings.TrimSpace(getenv(account.AccessTokenEnv))
		}
		if account.AccessKeyEnv != "" {
			account.AccessKey = strings.TrimSpace(getenv(account.AccessKeyEnv))
		}
		if account.SecretKeyEnv != "" {
			account.SecretKey = strings.TrimSpace(getenv(account.SecretKeyEnv))
		}
		if err := validateCredential(account, fieldPrefix, index); err != nil {
			return Config{}, err
		}
		cfg.Plans = append(cfg.Plans, account)
	}
	return cfg, nil
}

func defaultPlanForProvider(provider Provider) string {
	switch provider {
	case ProviderMiniMax:
		return "coding-plan"
	case ProviderArk:
		return "coding-plan"
	case ProviderCodex, ProviderGemini:
		return "oauth"
	case ProviderOpenCode:
		return "go"
	case ProviderGrok:
		return "api-key"
	default:
		return "api-key"
	}
}

func validateCredential(account PlanConfig, fieldPrefix string, index int) error {
	if account.Provider == ProviderArk {
		if account.AccessKeyEnv == "" {
			return fmt.Errorf("%s[%d].access_key_env is required", fieldPrefix, index)
		}
		if account.SecretKeyEnv == "" {
			return fmt.Errorf("%s[%d].secret_key_env is required", fieldPrefix, index)
		}
		if account.AccessKey == "" {
			return fmt.Errorf("environment variable %q is empty", account.AccessKeyEnv)
		}
		if account.SecretKey == "" {
			return fmt.Errorf("environment variable %q is empty", account.SecretKeyEnv)
		}
		return nil
	}
	if account.APIKeyEnv == "" && account.AccessTokenEnv == "" {
		return fmt.Errorf("%s[%d].api_key_env is required", fieldPrefix, index)
	}
	if account.APIKeyEnv != "" && account.APIKey == "" && account.AccessTokenEnv == "" {
		return fmt.Errorf("environment variable %q is empty", account.APIKeyEnv)
	}
	if account.AccessTokenEnv != "" && account.AccessToken == "" && account.APIKeyEnv == "" {
		return fmt.Errorf("environment variable %q is empty", account.AccessTokenEnv)
	}
	return nil
}

func validateProviderEndpoint(provider Provider, endpoint string) error {
	allowed, endpointAllowed := allowedEndpointHosts[provider]
	if !endpointAllowed {
		return fmt.Errorf("provider %q does not support endpoint overrides", provider)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("must be an HTTPS URL with a host")
	}
	if parsed.Host != strings.ToLower(parsed.Host) {
		return fmt.Errorf("host must be lowercase")
	}
	for _, host := range allowed {
		if parsed.Hostname() == host {
			return nil
		}
	}
	return fmt.Errorf("host must be one of %s", strings.Join(allowed, ", "))
}

func validateHTTPSEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("must be an HTTPS URL with a host")
	}
	return nil
}
