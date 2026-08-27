package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const (
	pluginID               = "quota-center"
	pluginName             = "额度中心"
	pluginVersion          = "0.2.0"
	managementStatusPath   = "/plugins/" + pluginID + "/status"
	managementAccountsPath = "/plugins/" + pluginID + "/accounts"
	// CPA passes the full browser resource path to management.handle.
	resourceStatusPath     = "/v0/resource/plugins/" + pluginID + "/status"
	maxAddAccountBodyBytes = 64 << 10
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

type registration struct {
	SchemaVersion uint32                   `json:"schema_version"`
	Metadata      pluginapi.Metadata       `json:"metadata"`
	Capabilities  registrationCapabilities `json:"capabilities"`
}

type registrationCapabilities struct {
	ManagementAPI bool `json:"management_api"`
}

type Dispatcher struct {
	mu          sync.Mutex
	config      Config
	client      QuotaClient
	service     *Service
	now         func() time.Time
	auth        AuthStore
	accounts    AuthStore
	cacheMu     sync.Mutex
	lastResults []PlanResult
	lastPlanIDs []string
}

func NewDispatcher(client QuotaClient) *Dispatcher {
	auth := hostAuthStore{}
	return &Dispatcher{
		config:   Config{Enabled: true, Priority: 1, Timeout: 15 * time.Second, Endpoint: defaultQuotaEndpoint},
		client:   client,
		now:      time.Now,
		auth:     auth,
		accounts: &fileAccountStore{host: auth},
	}
}

func NewTestRPC() *Dispatcher {
	return NewDispatcher(NewClient(QuotaHTTPClientFunc(func(context.Context, *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusServiceUnavailable, `{"msg":"test transport disabled"}`), nil
	})))
}

func (d *Dispatcher) Call(method string, request []byte) ([]byte, error) {
	if d == nil {
		return nil, fmt.Errorf("dispatcher unavailable")
	}
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if err := d.configure(request); err != nil {
			return nil, err
		}
		return okEnvelope(d.registration()), nil
	case pluginabi.MethodManagementRegister:
		return okEnvelope(managementRegistration()), nil
	case pluginabi.MethodManagementHandle:
		var req pluginapi.ManagementRequest
		if len(request) != 0 {
			if err := json.Unmarshal(request, &req); err != nil {
				return nil, fmt.Errorf("decode management request: %w", err)
			}
		}
		return okEnvelope(d.handleManagement(req)), nil
	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
}

func (d *Dispatcher) configure(request []byte) error {
	var req lifecycleRequest
	if len(request) != 0 {
		if err := json.Unmarshal(request, &req); err != nil {
			return fmt.Errorf("decode lifecycle request: %w", err)
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	cfg, err := DecodeConfig(req.ConfigYAML, getEnvironment)
	if err != nil {
		return err
	}
	client := d.client
	if existing, ok := d.client.(*Client); ok {
		client = NewClient(existing.httpDo)
		if typed, ok := client.(*Client); ok {
			typed.SetEndpoint(cfg.Endpoint)
		}
	}
	d.config = cfg
	d.client = client
	d.service = NewService(cfg, client)
	if d.accounts == nil {
		d.accounts = &fileAccountStore{host: d.auth}
	}
	auth, accounts, service := d.auth, d.accounts, d.service
	d.mu.Unlock()
	_ = migratePluginOwnedAuthFiles(context.Background(), auth, accounts)
	d.loadSavedAccountQuotas(cfg, client, service, auth)
	d.mu.Lock()
	return nil
}

func (d *Dispatcher) loadSavedAccountQuotas(config Config, client QuotaClient, service *Service, auth AuthStore) {
	if service == nil {
		service = NewService(config, client)
	}
	ctx, cancel := contextWithTimeout(context.Background(), config.Timeout)
	defer cancel()
	plans := d.resolvePlans(ctx, config.Plans, auth)
	if len(plans) == 0 {
		return
	}
	_ = refreshResults(d, service, ctx, plans, "")
}

func (d *Dispatcher) registration() registration {
	return registration{
		SchemaVersion: 1,
		Metadata: pluginapi.Metadata{
			Name:             pluginName,
			Version:          pluginVersion,
			Author:           "rayen",
			GitHubRepository: "https://github.com/RayenAlex/cpa-plugin",
			ConfigFields: []pluginapi.ConfigField{
				{Name: "accounts", Type: pluginapi.ConfigFieldTypeArray, Description: "Multi-provider accounts. Use provider, plan, label and a credential env field."},
				{Name: "plans", Type: pluginapi.ConfigFieldTypeArray, Description: "Legacy Zhipu plans; converted to provider zhipu accounts."},
				{Name: "timeout", Type: pluginapi.ConfigFieldTypeString, Description: "Quota request timeout, for example 15s."},
				{Name: "endpoint", Type: pluginapi.ConfigFieldTypeString, Description: "HTTPS Zhipu quota endpoint override."},
			},
		},
		Capabilities: registrationCapabilities{ManagementAPI: true},
	}
}

func managementRegistration() pluginapi.ManagementRegistrationResponse {
	return pluginapi.ManagementRegistrationResponse{
		Resources: []pluginapi.ResourceRoute{{
			Path: "/status", Menu: pluginName, Description: "多供应商额度看板。",
		}},
		Routes: []pluginapi.ManagementRoute{
			{Method: http.MethodGet, Path: managementStatusPath, Description: "Multi-provider quota dashboard."},
			{Method: http.MethodPost, Path: managementAccountsPath, Description: "Validate and save a provider account."},
			{Method: http.MethodDelete, Path: managementAccountsPath, Description: "Delete a manually added provider account."},
		},
	}
}

func (d *Dispatcher) handleManagement(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	path := strings.TrimSpace(req.Path)
	if strings.HasPrefix(path, "/v0/management") {
		path = strings.TrimPrefix(path, "/v0/management")
	}
	if path == managementAccountsPath && req.Method == http.MethodPost {
		return d.handleAddAccount(req)
	}
	if path == managementAccountsPath && req.Method == http.MethodDelete {
		return d.handleDeleteAccount(req)
	}
	if req.Method != http.MethodGet || (path != managementStatusPath && path != resourceStatusPath) {
		return jsonManagementResponse(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	if path == resourceStatusPath {
		// Browser resources are intentionally unauthenticated by CPA; never touch credentials there.
		// Do not render account metadata or quota snapshots without management authorization.
		return htmlManagementResponse(RenderResourcePanel(nowUTC(d.now)))
	}
	d.mu.Lock()
	config, client, service := d.config, d.client, d.service
	d.mu.Unlock()
	requestService := service
	if requestService == nil {
		if existing, ok := client.(*Client); ok {
			client = NewClient(existing.httpDo)
			if typed, ok := client.(*Client); ok {
				typed.SetEndpoint(config.Endpoint)
			}
		}
		requestService = NewService(config, client)
	}
	ctx, cancel := contextWithTimeout(context.Background(), config.Timeout)
	defer cancel()
	plans := d.resolvePlans(ctx, config.Plans, d.auth)
	results := refreshResults(d, requestService, ctx, plans, req.Query.Get("refresh"))
	view := req.Query.Get("view")
	return htmlManagementResponse(RenderPanelView(results, nowUTC(d.now), view))
}

type addAccountRequest struct {
	ID              string `json:"id"`
	Provider        string `json:"provider"`
	Plan            string `json:"plan"`
	Label           string `json:"label"`
	Credential      string `json:"credential"`
	AccessID        string `json:"access_id"`
	AccessKeyID     string `json:"access_key_id"`
	Secret          string `json:"secret"`
	SecretAccessKey string `json:"secret_access_key"`
	Endpoint        string `json:"endpoint"`
}

func (d *Dispatcher) handleAddAccount(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	if len(req.Body) > maxAddAccountBodyBytes {
		return jsonManagementResponse(http.StatusRequestEntityTooLarge, map[string]string{"error": "request body is too large"})
	}
	var input addAccountRequest
	if err := json.Unmarshal(req.Body, &input); err != nil {
		return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	provider := Provider(strings.ToLower(strings.TrimSpace(input.Provider)))
	if _, ok := supportedProviders[provider]; !ok {
		return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "unsupported provider"})
	}
	if !isPluginManagedProvider(provider) {
		return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "CPA 原生账号请用同步登录，不要写入认证文件"})
	}
	endpoint := strings.TrimSpace(input.Endpoint)
	if endpoint != "" {
		if err := validateProviderEndpoint(provider, endpoint); err != nil {
			return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "endpoint is invalid"})
		}
	}
	label := strings.TrimSpace(input.Label)
	if label == "" {
		return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "label is required"})
	}
	existingID := strings.TrimSpace(input.ID)
	if existingID != "" {
		if !planIDPattern.MatchString(existingID) {
			return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "account id is invalid"})
		}
		lookupCtx, lookupCancel := contextWithTimeout(context.Background(), time.Second)
		existing, ok := d.lookupPlan(lookupCtx, existingID)
		lookupCancel()
		if ok && existing.Source == AuthSourceCPA {
			return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "CPA 原生账号请在 CPA 重新登录，不能在此覆盖"})
		}
	}
	accountID := existingID
	if accountID == "" {
		accountID = makeAccountID(provider, label)
	}
	if !planIDPattern.MatchString(accountID) {
		return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "label is too long"})
	}
	plan := PlanConfig{ID: accountID, Label: label, Provider: provider, Plan: strings.TrimSpace(input.Plan), Endpoint: endpoint, AccessKey: firstNonEmpty(input.AccessID, input.AccessKeyID), SecretKey: firstNonEmpty(input.Secret, input.SecretAccessKey)}
	plan = normalizePlan(plan)
	credentialValue := strings.TrimSpace(input.Credential)
	switch provider {
	case ProviderArk:
		if plan.AccessKey == "" || plan.SecretKey == "" {
			return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "Ark requires AccessKey ID and Secret AccessKey"})
		}
	case ProviderCodex, ProviderGemini:
		if credentialValue == "" {
			return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "credential is required"})
		}
		plan.AccessToken = credentialValue
	case ProviderGrok:
		if credentialValue == "" {
			return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "credential is required"})
		}
		plan.AccessToken = credentialValue
	default:
		if credentialValue == "" {
			return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "credential is required"})
		}
		plan.APIKey = credentialValue
	}
	d.mu.Lock()
	config, client, service := d.config, d.client, d.service
	d.mu.Unlock()
	requestService := service
	if requestService == nil {
		requestService = NewService(config, client)
	}
	ctx, cancel := contextWithTimeout(context.Background(), config.Timeout)
	defer cancel()
	remembered := PlanResult{ID: plan.ID, Provider: provider, Plan: plan.Plan, Label: plan.Label, Endpoint: plan.Endpoint, CredentialMask: maskCredential(firstNonEmpty(plan.APIKey, plan.AccessKey))}
	if provider != ProviderOpenCode {
		remembered = requestService.RefreshOne(ctx, plan)
		if remembered.Error != "" {
			return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": remembered.Error})
		}
	}
	credential := map[string]any{
		"id":       plan.ID,
		"provider": string(provider),
		"type":     string(provider),
		"plan":     plan.Plan,
		"label":    plan.Label,
	}
	if plan.Endpoint != "" {
		credential["endpoint"] = plan.Endpoint
	}
	if provider == ProviderArk {
		credential["access_key_id"] = plan.AccessKey
		credential["secret_access_key"] = plan.SecretKey
	} else if provider == ProviderCodex || provider == ProviderGemini || provider == ProviderGrok {
		credential["access_token"] = plan.AccessToken
	} else {
		credential["api_key"] = plan.APIKey
	}
	rawCredential, _ := json.Marshal(credential)
	store := d.accountStore()
	if store == nil {
		return jsonManagementResponse(http.StatusServiceUnavailable, map[string]string{"error": "account storage unavailable"})
	}
	if _, err := store.Save(ctx, plan.ID+".json", rawCredential); err != nil {
		return jsonManagementResponse(http.StatusServiceUnavailable, map[string]string{"error": "account storage unavailable"})
	}
	d.rememberResult(remembered)
	return jsonManagementResponse(http.StatusCreated, map[string]any{
		"ok":              true,
		"id":              plan.ID,
		"provider":        provider,
		"plan":            plan.Plan,
		"label":           plan.Label,
		"credential_mask": maskCredential(firstNonEmpty(plan.APIKey, plan.AccessKey)),
	})
}

func (d *Dispatcher) handleDeleteAccount(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	id := strings.TrimSpace(req.Query.Get("id"))
	if id == "" && len(req.Body) > 0 {
		var input struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Body, &input); err == nil {
			id = strings.TrimSpace(input.ID)
		}
	}
	id = strings.TrimSuffix(id, ".json")
	if id == "" || !planIDPattern.MatchString(id) {
		return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "account id is required"})
	}
	ctx, cancel := contextWithTimeout(context.Background(), time.Second)
	defer cancel()
	if existing, ok := d.lookupPlan(ctx, id); ok && existing.Source == AuthSourceCPA {
		return jsonManagementResponse(http.StatusBadRequest, map[string]string{"error": "CPA 原生账号请在 CPA 退出登录，不能在此删除"})
	}
	store := d.accountStore()
	if store == nil {
		return jsonManagementResponse(http.StatusServiceUnavailable, map[string]string{"error": "account storage unavailable"})
	}
	deleter, ok := store.(interface {
		Delete(context.Context, string) error
	})
	if !ok {
		return jsonManagementResponse(http.StatusServiceUnavailable, map[string]string{"error": "account delete unavailable"})
	}
	if err := deleter.Delete(ctx, id+".json"); err != nil {
		return jsonManagementResponse(http.StatusServiceUnavailable, map[string]string{"error": "account delete unavailable"})
	}
	d.forgetResult(id)
	return jsonManagementResponse(http.StatusOK, map[string]any{"ok": true, "id": id})
}

func (d *Dispatcher) lookupPlan(ctx context.Context, id string) (PlanConfig, bool) {
	d.mu.Lock()
	plans := d.config.Plans
	auth := d.auth
	d.mu.Unlock()
	want := strings.TrimSuffix(strings.TrimSpace(id), ".json")
	for _, plan := range d.resolvePlans(ctx, plans, auth) {
		if plan.ID == want || strings.TrimSuffix(plan.ID, ".json") == want {
			return plan, true
		}
	}
	return PlanConfig{}, false
}

func (d *Dispatcher) accountStore() AuthStore {
	if d != nil && d.accounts != nil {
		return d.accounts
	}
	if d != nil {
		return d.auth
	}
	return nil
}

func (d *Dispatcher) resolvePlans(ctx context.Context, configuredPlans []PlanConfig, auth AuthStore) []PlanConfig {
	plans := append([]PlanConfig(nil), configuredPlans...)
	seen := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		seen[plan.ID] = struct{}{}
	}
	appendStore := func(store AuthStore) {
		if store == nil {
			return
		}
		entries, err := store.List(ctx)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.AuthIndex != "" {
				if detail, getErr := store.Get(ctx, entry.AuthIndex); getErr == nil {
					detail.AuthIndex = firstNonEmpty(detail.AuthIndex, entry.AuthIndex)
					detail.Name = firstNonEmpty(detail.Name, entry.Name)
					detail.Provider = firstNonEmpty(string(detail.Provider), string(entry.Provider))
					detail.Label = firstNonEmpty(detail.Label, entry.Label)
					detail.Disabled = detail.Disabled || entry.Disabled
					detail.Path = firstNonEmpty(detail.Path, entry.Path)
					entry = detail
				}
			}
			plan, err := accountFromStoredCredential(entry)
			if err != nil || plan.ID == "" {
				continue
			}
			if _, exists := seen[plan.ID]; exists {
				continue
			}
			seen[plan.ID] = struct{}{}
			plans = append(plans, plan)
		}
	}
	appendStore(d.accountStore())
	if auth != d.accountStore() {
		appendStore(auth)
	}
	for index, plan := range plans {
		if plan.AuthIndex == "" || (plan.APIKey != "" && plan.AccessToken != "" && plan.AccessKey != "") {
			continue
		}
		for _, store := range []AuthStore{d.accountStore(), auth} {
			if store == nil {
				continue
			}
			entry, err := store.Get(ctx, plan.AuthIndex)
			if err != nil {
				continue
			}
			resolved, err := accountFromStoredCredential(entry)
			if err == nil {
				resolved.ID = plan.ID
				resolved.Label = plan.Label
				plans[index] = resolved
				break
			}
		}
	}
	return plans
}

func refreshResults(d *Dispatcher, service *Service, ctx context.Context, plans []PlanConfig, refreshID string) []PlanResult {
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()
	planIDs := make([]string, len(plans))
	for index, plan := range plans {
		planIDs[index] = plan.ID
	}
	cacheMatches := len(d.lastPlanIDs) == len(planIDs)
	for index := range planIDs {
		if cacheMatches && (d.lastPlanIDs[index] != planIDs[index] || (index < len(d.lastResults) && d.lastResults[index].Endpoint != plans[index].Endpoint)) {
			cacheMatches = false
		}
	}
	if refreshID != "" && cacheMatches && len(d.lastResults) == len(plans) {
		for index, plan := range plans {
			if plan.ID == refreshID {
				d.lastResults[index] = service.RefreshOne(ctx, plan)
				break
			}
		}
		return append([]PlanResult(nil), d.lastResults...)
	}
	d.lastPlanIDs = planIDs
	d.lastResults = service.RefreshAll(ctx, plans)
	return append([]PlanResult(nil), d.lastResults...)
}

func (d *Dispatcher) forgetResult(id string) {
	id = strings.TrimSuffix(strings.TrimSpace(id), ".json")
	if id == "" {
		return
	}
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()
	results := d.lastResults[:0]
	ids := d.lastPlanIDs[:0]
	for _, result := range d.lastResults {
		if result.ID == id {
			continue
		}
		results = append(results, result)
		ids = append(ids, result.ID)
	}
	d.lastResults = results
	d.lastPlanIDs = ids
}

func (d *Dispatcher) rememberResult(result PlanResult) {
	if result.ID == "" {
		return
	}
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()
	for index, existing := range d.lastResults {
		if existing.ID == result.ID {
			d.lastResults[index] = result
			if index < len(d.lastPlanIDs) {
				d.lastPlanIDs[index] = result.ID
			}
			return
		}
	}
	d.lastResults = append(d.lastResults, result)
	d.lastPlanIDs = append(d.lastPlanIDs, result.ID)
}

func (d *Dispatcher) refreshResults(ctx context.Context, plans []PlanConfig, refreshID string) []PlanResult {
	service := d.service
	if service == nil {
		service = NewService(d.config, d.client)
	}
	return refreshResults(d, service, ctx, plans, refreshID)
}

func makeAccountID(provider Provider, label string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(string(provider) + "-" + label) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
		} else if builder.Len() == 0 || !strings.HasSuffix(builder.String(), "-") {
			builder.WriteByte('-')
		}
	}
	value := strings.Trim(builder.String(), "-._")
	if value == "" {
		return string(provider) + "-account"
	}
	return value
}
