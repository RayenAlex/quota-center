package main

import (
	"context"
	"strings"
	"sync"
	"time"
)

type Service struct {
	cfg    Config
	client QuotaClient
	now    func() time.Time
}

type PlanResult struct {
	ID             string       `json:"id"`
	Provider       Provider     `json:"provider"`
	Plan           string       `json:"plan,omitempty"`
	Label          string       `json:"label"`
	Source         AuthSource   `json:"source,omitempty"`
	AuthType       string       `json:"auth_type,omitempty"`
	CredentialMask string       `json:"credential_mask,omitempty"`
	Endpoint       string       `json:"-"`
	Quota          *QuotaResult `json:"quota,omitempty"`
	Error          string       `json:"error,omitempty"`
	FetchedAt      time.Time    `json:"fetched_at"`
}

func NewService(cfg Config, client QuotaClient) *Service {
	if client, ok := client.(*Client); ok {
		client.SetEndpoint(cfg.Endpoint)
	}
	return &Service{cfg: cfg, client: client, now: time.Now}
}

func (s *Service) refresh(ctx context.Context, plan PlanConfig) PlanResult {
	plan = normalizePlan(plan)
	result := PlanResult{
		ID:             plan.ID,
		Provider:       plan.Provider,
		Plan:           plan.Plan,
		Label:          plan.Label,
		Source:         plan.Source,
		AuthType:       plan.AuthType,
		Endpoint:       plan.Endpoint,
		CredentialMask: maskCredential(firstNonEmpty(plan.APIKey, plan.AccessToken, plan.AccessKey)),
	}
	if s.now != nil {
		result.FetchedAt = s.now().UTC()
	}
	if plan.Disabled {
		result.Error = "CPA 认证已禁用，请在 CPA 重新启用或重新登录"
		return result
	}
	if plan.Source == AuthSourceCPA {
		switch plan.Provider {
		case ProviderCodex, ProviderGemini, ProviderGrok:
			if strings.TrimSpace(plan.AccessToken) == "" && strings.TrimSpace(plan.APIKey) == "" {
				result.Error = "CPA 原生认证缺少可用 Token，请在 CPA 重新登录或刷新"
				return result
			}
		}
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	quota, err := s.client.FetchQuota(requestCtx, plan)
	cancel()
	if err != nil {
		result.Error = redactSecrets(err.Error(), plan)
	} else {
		result.Quota = quota
	}
	return result
}

func (s *Service) RefreshAll(ctx context.Context, plans []PlanConfig) []PlanResult {
	results := make([]PlanResult, len(plans))
	var wait sync.WaitGroup
	for index, plan := range plans {
		wait.Add(1)
		go func(index int, plan PlanConfig) {
			defer wait.Done()
			results[index] = s.refresh(ctx, plan)
		}(index, plan)
	}
	wait.Wait()
	return results
}

func (s *Service) RefreshOne(ctx context.Context, plan PlanConfig) PlanResult {
	return s.refresh(ctx, plan)
}

func normalizePlan(plan PlanConfig) PlanConfig {
	if plan.Provider == "" {
		plan.Provider = ProviderZhipu
	}
	if plan.Plan == "" {
		plan.Plan = defaultPlanForProvider(plan.Provider)
	}
	return plan
}

func redactSecrets(message string, plan PlanConfig) string {
	for _, secret := range []string{plan.APIKey, plan.AccessToken, plan.AccessKey, plan.SecretKey} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "••••••••")
		}
	}
	return message
}
