package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRenderPanelUsesQuotaCenterBranding(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	if !strings.Contains(html, "<title>额度中心</title>") || !strings.Contains(html, ">额度中心<") {
		t.Fatal("panel missing 额度中心 branding")
	}
	if strings.Contains(html, "Zhipu Quota") || strings.Contains(html, "Quota Center") {
		t.Fatal("panel still uses Zhipu/Quota Center product name")
	}
}

func TestRenderPanelShowsAllPlansAndRedactsKeys(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	html := RenderPanel([]PlanResult{
		{ID: "pro", Label: "GLM Pro", Error: "Zhipu API 401: unauthorized", FetchedAt: at},
		{ID: "max", Label: "GLM Max", Quota: &QuotaResult{Level: "max", FiveHour: QuotaWindow{UsedPercent: 20, RemainingPercent: 80}}, FetchedAt: at},
	}, at)
	for _, want := range []string{"GLM Pro", "GLM Max", "80.0%", "Zhipu API 401: unauthorized"} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing %q", want)
		}
	}
	for _, leaked := range []string{"api_key", "access_token", "secret-key"} {
		if strings.Contains(html, leaked) {
			t.Fatalf("panel leaked key-like text %q", leaked)
		}
	}
}

func TestRenderPanelProviderControlsExposeNativeSelection(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	for _, want := range []string{
		`id="quota-panel"`,
		`name="provider"`,
		`value="zhipu" data-provider="zhipu" checked`,
		`value="minimax"`,
		`value="opencode-go"`,
		`value="ark"`,
		`.qc-provider-input:checked + .qc-provider`,
		`form.elements.provider.value`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing native provider selection contract %q", want)
		}
	}
	if strings.Contains(html, `document.querySelector('[data-provider].active')`) {
		t.Fatal("provider submission should not depend on a transient active class")
	}
}

func TestRenderPanelAddConnectionOmitsNativeProviders(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	for _, leaked := range []string{
		`value="grok"`,
		`value="codex"`,
		`value="gemini"`,
		`const nativeProviders=['codex','gemini','grok']`,
	} {
		if strings.Contains(html, leaked) {
			t.Fatalf("add-connection form should omit native provider %q", leaked)
		}
	}
	if !strings.Contains(html, `开始配置智谱、MiniMax、方舟`) {
		t.Fatal("empty-state copy should not invite adding Grok/Codex/Gemini")
	}
}

func TestRenderPanelExplainsManagementAuthorizationFailure(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	want := `if(response.status===401){alert('CPA 管理授权未传入插件窗口，无法保存；Grok/Codex/Gemini 已登录 CPA 时无需重复保存。');return}`
	if !strings.Contains(html, want) {
		t.Fatal("panel must distinguish management authorization failure from provider credential failure")
	}
}

func TestRenderPanelReusesCPAManagementAuthorization(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	for _, want := range []string{
		`readStorageItem(store,'cli-proxy-auth')`,
		`deobfuscatePanelValue(authRaw)`,
		`parsed?.state?.managementKey`,
		`Authorization:'Bearer '+key`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing management authorization bridge %q", want)
		}
	}
}

func TestRenderPanelDoesNotGuessGenericManagementPasswords(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	for _, forbidden := range []string{
		`management_password`,
		`CPA_MANAGEMENT_KEY`,
		`managementKey','cli-proxy-management-key`,
		`parsed.managementKey||parsed.password||parsed.key`,
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("panel includes generic management secret guess %q", forbidden)
		}
	}
}

func TestRenderPanelSyncReplacesResourceDocumentWithManagementResponse(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	for _, want := range []string{
		`document.open();document.write(html);document.close()`,
		`alert('同步 CPA 登录失败：'+error.message)`,
		`syncButton.disabled=true`,
		`<button class="qc-cancel" type="button" data-reload-accounts>同步 CPA 登录账号</button>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing sync response handling %q", want)
		}
	}
	if strings.Contains(html, `<div class="meta"><span>{{len .Plans}} 个连接</span><span>最后更新 {{.GeneratedAt.Format "2006-01-02 15:04:05 UTC"}}</span><button class="refresh" type="button" data-reload-accounts>同步 CPA 登录账号</button>`) {
		t.Fatal("sync button must leave the page header")
	}
	meta := `<div class="meta"><span>0 个连接</span>`
	if i := strings.Index(html, meta); i >= 0 {
		chunk := html[i : i+220]
		if strings.Contains(chunk, `data-reload-accounts`) {
			t.Fatal("sync button must move into the add-connection modal")
		}
	}
	if strings.Contains(html, `then(()=>location.href=url.toString())`) {
		t.Fatal("sync must render the authenticated response instead of navigating without Authorization")
	}
}

func TestRenderPanelReadsOnlyCPAManagementAuthState(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	for _, want := range []string{
		`readStorageItem(store,'cli-proxy-auth')`,
		`parsed?.state?.managementKey`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing CPA management auth bridge %q", want)
		}
	}
}

func TestRenderPanelUsesScopedDarkControlPalette(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	for _, want := range []string{
		`--qc-bg:#00120f`,
		`--qc-panel:#06251f`,
		`--qc-text:#d8f5e7`,
		`--qc-accent:#42d89e`,
		`html.cpamp-plugin-host`,
		`html[data-cpamp-plugin-host]`,
		`body#quota-panel`,
		`#quota-panel .main`,
		`#quota-panel .card`,
		`#quota-panel .action`,
		`.qc-provider`,
		`.qc-input`,
		`.qc-select`,
		`appearance:none`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing scoped palette/control style %q", want)
		}
	}
	for _, leaked := range []string{
		`--bg:#00120f`,
		`--panel:#06251f`,
		`:root{color-scheme:dark;--host-header-safe-area`,
	} {
		if strings.Contains(html, leaked) {
			t.Fatalf("panel still exposes host-colliding token %q", leaked)
		}
	}
}

func TestRenderPanelReservesHostHeaderSafeArea(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	for _, want := range []string{
		`--qc-host-header-safe-area:96px`,
		`.main{min-width:0;padding:calc(var(--qc-host-header-safe-area) + 28px) clamp(18px,4vw,42px) 48px}`,
		`@media(max-width:650px){.app{grid-template-columns:1fr}`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing host header safe-area contract %q", want)
		}
	}
}

func TestRenderPanelHidesGenericCredentialForArk(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	for _, want := range []string{
		`arkFields.forEach(field=>field.hidden=provider!=='ark')`,
		`credentialField.hidden=provider==='ark'`,
		`方舟使用下方 AccessKey ID / Secret`,
		`.qc-field[hidden],.qc-ark-only[hidden]{display:none!important}`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing Ark-only credential contract %q", want)
		}
	}
}

func TestRenderPanelRequiresExplicitManagementSync(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	if !strings.Contains(html, `data-reload-accounts`) {
		t.Fatal("panel must keep an explicit CPA account sync action")
	}
	for _, forbidden := range []string{
		`if(!document.querySelector('[data-account-id]')`,
		`sessionStorage.getItem('qc-auto-synced-0.2.0')`,
		`sessionStorage.setItem('{{.AutoSyncKey}}','1')`,
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("public resource must not auto-promote itself with management credentials: %q", forbidden)
		}
	}
}

func TestRenderStatusJSON(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	raw := RenderStatusJSON([]PlanResult{{ID: "pro", Label: "GLM Pro", FetchedAt: at}})
	var payload struct {
		GeneratedAt time.Time `json:"generated_at"`
		Plans       []struct {
			ID string `json:"id"`
		} `json:"plans"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if !payload.GeneratedAt.Equal(at) || len(payload.Plans) != 1 || payload.Plans[0].ID != "pro" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestRenderPanelSurfacesAddAccountErrorDetail(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	for _, want := range []string{
		`const data=await response.json()`,
		`data&&data.error`,
		`submit.disabled=true`,
		`验证中`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing add-account error contract %q", want)
		}
	}
	if strings.Contains(html, `if(!response.ok){alert('保存失败，请检查凭据或管理权限');return}`) {
		t.Fatal("add-account failure must show the server error instead of a generic alert")
	}
}

func TestRenderPanelReloadsManagementViewAfterAddAccount(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	for _, want := range []string{
		`const loadManagementPanel=async(view)=>{`,
		`if(typeof view==='string'&&view)url.searchParams.set('view',view)`,
		`await loadManagementPanel('accounts')`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing post-add management reload %q", want)
		}
	}
	if strings.Contains(html, "location.href='?view=accounts'") {
		t.Fatal("add-account success must not navigate the unauthenticated resource URL")
	}
}

func TestRenderPanelAccountsViewCanEditAndDeleteManualAccounts(t *testing.T) {
	html := RenderPanelView([]PlanResult{
		{ID: "ark-work", Provider: ProviderArk, Label: "工作方舟", Source: AuthSourceConfig},
		{ID: "codex-index", Provider: ProviderCodex, Label: "Codex", Source: AuthSourceCPA},
	}, time.Unix(100, 0).UTC(), "accounts")
	for _, want := range []string{
		`data-edit="ark-work"`,
		`data-delete="ark-work"`,
		`data-provider="ark"`,
		`data-label="工作方舟"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("accounts view missing manual edit/delete %q", want)
		}
	}
	if strings.Contains(html, `data-edit="codex-index"`) || strings.Contains(html, `data-delete="codex-index"`) {
		t.Fatal("CPA native accounts must not expose plugin edit/delete")
	}
}

func TestRenderPanelEditAndDeleteUseManagementRoutes(t *testing.T) {
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	for _, want := range []string{
		`name="account_id"`,
		`编辑供应商连接`,
		`/v0/management/plugins/quota-center/accounts?id=`,
		`id:form.elements.account_id?.value||''`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing edit/delete contract %q", want)
		}
	}
	if strings.Contains(html, `/v0/management/auth-files`) {
		t.Fatal("plugin-managed accounts must delete via plugin storage, not require CPA auth-files")
	}
}

func TestRenderPanelLocalizesResetAndGeneratedTimes(t *testing.T) {
	reset := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	html := RenderPanelView([]PlanResult{{
		ID:    "pro",
		Label: "GLM Pro",
		Quota: &QuotaResult{Windows: []QuotaWindow{{Name: "five_hour", RemainingPercent: 80, ResetAt: &reset}}},
	}}, time.Date(2026, 8, 15, 15, 4, 5, 0, time.UTC), "overview")
	for _, want := range []string{
		`datetime="2026-08-15T15:04:05Z"`,
		`datetime="2026-08-16T00:00:00Z"`,
		`time[data-local-time]`,
		`toLocaleString(undefined,`,
		`timeZoneName:'short'`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing local timezone contract %q", want)
		}
	}
	if strings.Contains(html, "15:04 UTC") || strings.Contains(html, "15:04:05 UTC") {
		t.Fatal("panel must not hardcode UTC labels for displayed times")
	}
}

func TestRenderPanelShowsAntigravityFamilyWindows(t *testing.T) {
	reset := time.Date(2026, 8, 16, 4, 20, 0, 0, time.UTC)
	html := RenderPanelView([]PlanResult{{
		ID:       "antigravity-user",
		Provider: ProviderGemini,
		AuthType: "antigravity",
		Label:    "qq620068782@gmail.com",
		Source:   AuthSourceCPA,
		Quota: &QuotaResult{Level: "Antigravity", Windows: []QuotaWindow{
			{Group: "Gemini 模型", Name: "Five Hour Limit Remaining", RemainingPercent: 100, ResetAt: &reset},
			{Group: "Claude 和 GPT 模型", Name: "Five Hour Limit Remaining", Available: true, RemainingPercent: 100, ResetAt: &reset},
		}},
	}}, time.Date(2026, 8, 15, 15, 4, 5, 0, time.UTC), "overview")
	for _, want := range []string{
		"Antigravity",
		"Gemini 模型",
		"Claude 和 GPT 模型",
		"Five Hour Limit Remaining",
		"额度可用",
		`data-relative-reset`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel missing antigravity quota contract %q", want)
		}
	}
	if strings.Contains(html, ">pro<") || strings.Contains(html, ">flash<") || strings.Contains(html, ">flash-lite<") {
		t.Fatal("antigravity quota must not display gemini-cli model buckets")
	}
}
