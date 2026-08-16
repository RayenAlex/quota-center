package main

import (
	"html/template"
	"strconv"
	"strings"
	"time"
)

type panelData struct {
	GeneratedAt time.Time
	Plans       []PlanResult
	View        string
	PluginID    string
}

var panelHelpers = template.FuncMap{
	"providerLabel": providerLabel,
	"accountBrand":  accountBrand,
	"windows":       displayWindows,
	"windowGroups":  displayWindowGroups,
	"remainingText": remainingText,
	"mask":          func(value string) string { return maskCredential(value) },
	"percent":       func(value float64) string { return formatPercent(value) },
	"providers":     func() []providerOption { return providerOptions },
}

var panelTemplate = template.Must(template.New("panel").Funcs(panelHelpers).Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>额度中心</title><style>
#quota-panel{color-scheme:dark;--qc-host-header-safe-area:96px;--qc-bg:#00120f;--qc-panel:#06251f;--qc-panel-2:#0a3029;--qc-control:#041b17;--qc-line:#1d6c5a;--qc-text:#d8f5e7;--qc-muted:#84bca9;--qc-accent:#42d89e;--qc-accent-strong:#155646;--qc-warn:#f1b46c;--qc-danger:#f08383}
*{box-sizing:border-box}body{margin:0;background:var(--qc-bg);color:var(--qc-text);font:14px/1.45 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}a{color:inherit;text-decoration:none}
.app{display:grid;grid-template-columns:164px minmax(0,1fr);min-height:100vh}.nav{padding:20px 12px;background:#0b2420;border-right:1px solid var(--qc-line)}.logo{padding:0 8px 22px;font-weight:850;letter-spacing:-.04em}.nav-group{display:grid;gap:6px}.nav-item{display:block;padding:11px;border-radius:9px;color:var(--qc-muted);font-size:11px;font-weight:750}.nav-item.active{background:#17483e;color:var(--qc-text)}.nav-item small{display:block;margin-top:3px;font-size:9px;font-weight:500;opacity:.58}
.main{min-width:0;padding:calc(var(--qc-host-header-safe-area) + 28px) clamp(18px,4vw,42px) 48px}.header{display:flex;justify-content:space-between;align-items:flex-start;gap:18px;padding-bottom:18px;border-bottom:1px solid var(--qc-line)}h1{margin:0;font-size:25px;letter-spacing:-.05em}h2{margin:0;font-size:18px}p{margin:5px 0 0;color:var(--qc-muted);font-size:11px}.action{border:0;border-radius:999px;padding:10px 14px;background:var(--qc-accent);color:#05251c;font:inherit;font-size:11px;font-weight:850;cursor:pointer}.meta{display:flex;gap:14px;flex-wrap:wrap;margin-top:14px;color:var(--qc-muted);font-size:10px}
.grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px;margin-top:18px}.card{min-width:0;padding:14px;border:1px solid var(--qc-line);border-radius:13px;background:var(--qc-panel)}.card-head{display:flex;justify-content:space-between;gap:10px}.card-title{font-weight:800;font-size:13px}.card-sub{margin-top:4px;color:var(--qc-muted);font-size:10px}.refresh{border:1px solid #286a5a;border-radius:7px;padding:5px 7px;background:transparent;color:#b6ecd8;font:inherit;font-size:10px;cursor:pointer}.card-actions{display:flex;gap:6px;margin-top:12px}.refresh:hover{background:#17483e}.window{margin-top:14px}.window-head{display:flex;justify-content:space-between;gap:8px;color:var(--qc-muted);font-size:10px}.remaining{color:var(--qc-text);font-variant-numeric:tabular-nums}.bar{height:7px;margin-top:6px;border-radius:4px;background:#1a3b34;overflow:hidden}.fill{height:100%;border-radius:inherit;background:var(--qc-accent)}.reset{margin-top:4px;color:var(--qc-muted);font-size:9px}.error{margin-top:14px;color:var(--qc-warn);font-size:11px}.status{display:inline-flex;align-items:center;gap:5px;color:var(--qc-accent);font-size:10px}.status:before{content:"";width:6px;height:6px;border-radius:50%;background:currentColor}.status.warn{color:var(--qc-warn)}.status.danger{color:var(--qc-danger)}.mask{margin-top:13px;border:1px solid #286a5a;border-radius:9px;padding:9px;color:#9ccdbb;background:#071918;font:600 10px ui-monospace,SFMono-Regular,Menlo,monospace;word-break:break-all}.empty{margin-top:18px;padding:28px;border:1px dashed #286a5a;border-radius:13px;text-align:center;color:var(--qc-muted)}.window-group{margin-top:10px}.window-group-title{margin:8px 0 2px;color:var(--qc-muted);font-size:10px;font-weight:700;text-align:center}
.qc-overlay{position:fixed;inset:0;display:grid;place-items:center;padding:22px;background:rgba(0,10,8,.74)}.qc-overlay[hidden]{display:none}.qc-modal{width:min(560px,100%);border:1px solid var(--qc-line);border-radius:16px;background:var(--qc-panel);box-shadow:0 24px 70px rgba(0,0,0,.45)}.qc-modal-head{display:flex;justify-content:space-between;align-items:flex-start;gap:12px;padding:18px 18px 14px;border-bottom:1px solid var(--qc-line)}.qc-modal-head h3{margin:0;font-size:16px}.qc-close{border:0!important;background:transparent!important;color:#9ccdbb!important;font-size:20px;cursor:pointer;appearance:none;-webkit-appearance:none}.qc-form{display:grid;gap:12px;padding:16px 18px 18px}.qc-field{display:grid;gap:6px}.qc-field[hidden],.qc-ark-only[hidden]{display:none!important}.qc-field label{font-size:10px;font-weight:800}.qc-field small,.qc-note{color:var(--qc-muted);font-size:9px}.qc-select,.qc-input{width:100%;border:1px solid var(--qc-line)!important;border-radius:9px;padding:10px;color:var(--qc-text)!important;background-color:var(--qc-control)!important;font:inherit;font-size:12px;appearance:none;-webkit-appearance:none}.qc-select option{background:var(--qc-panel);color:var(--qc-text)}.qc-provider-list{display:flex;flex-wrap:wrap;gap:6px}.qc-provider-input{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);clip-path:inset(50%);white-space:nowrap}.qc-provider{display:inline-flex;align-items:center;border:1px solid var(--qc-line)!important;border-radius:999px;padding:6px 8px;color:#9ccdbb!important;background-color:transparent!important;font-size:10px;font-weight:750;cursor:pointer;appearance:none;-webkit-appearance:none}.qc-provider-input:checked + .qc-provider,.qc-provider:hover{background-color:var(--qc-accent-strong)!important;color:var(--qc-text)!important}.qc-provider-input:focus-visible + .qc-provider{outline:2px solid var(--qc-accent);outline-offset:2px}.qc-actions{display:flex;justify-content:flex-end;gap:8px;margin-top:3px}.qc-cancel,.qc-submit{border-radius:9px;padding:9px 13px;font:inherit;font-size:11px;font-weight:850;cursor:pointer;appearance:none;-webkit-appearance:none}.qc-cancel{border:1px solid var(--qc-line)!important;background:transparent!important;color:#b6ecd8!important}.qc-submit{border:0!important;background:var(--qc-accent)!important;color:#05251c!important}
@media(max-width:900px){.grid{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:650px){.app{grid-template-columns:1fr}.nav{display:flex;gap:8px;align-items:center;border-right:0;border-bottom:1px solid var(--qc-line)}.logo{padding:0 10px 0 0}.nav-group{display:flex}.nav-item{white-space:nowrap}.grid{grid-template-columns:1fr}}
html.cpamp-plugin-host,html[data-cpamp-plugin-host],html.cpamp-plugin-host body,body#quota-panel,#quota-panel,#quota-panel .app,#quota-panel .main{background:#00120f!important;color:#d8f5e7!important;color-scheme:dark!important}
#quota-panel a{color:inherit!important;text-decoration:none!important}
#quota-panel h1,#quota-panel h2,#quota-panel h3,#quota-panel .card-title,#quota-panel .logo,#quota-panel .remaining{color:#d8f5e7!important}
#quota-panel p,#quota-panel small,#quota-panel label,#quota-panel .card-sub,#quota-panel .meta,#quota-panel .reset,#quota-panel .empty,#quota-panel .qc-note,#quota-panel .nav-item,#quota-panel .window-head,#quota-panel .window-group-title{color:#84bca9!important}
#quota-panel .nav{background:#0b2420!important;border-color:#1d6c5a!important}
#quota-panel .nav-item.active{background:#17483e!important;color:#d8f5e7!important}
#quota-panel .card,#quota-panel .qc-modal,#quota-panel .empty{background:#06251f!important;color:#d8f5e7!important;border-color:#1d6c5a!important}
#quota-panel .card-head,#quota-panel .card-title,#quota-panel .card-sub,#quota-panel .card-actions,#quota-panel .window,#quota-panel .window-head{background:transparent!important}
#quota-panel .action,#quota-panel .qc-submit{background:#42d89e!important;color:#05251c!important;border:0!important}
#quota-panel .refresh,#quota-panel .qc-cancel{background:transparent!important;color:#b6ecd8!important;border:1px solid #286a5a!important}
#quota-panel .qc-close{background:transparent!important;color:#9ccdbb!important;border:0!important}
#quota-panel .qc-input,#quota-panel .qc-select{background:#041b17!important;color:#d8f5e7!important;border:1px solid #1d6c5a!important}
#quota-panel .qc-provider{background:transparent!important;color:#9ccdbb!important;border:1px solid #1d6c5a!important}
#quota-panel .qc-provider-input:checked + .qc-provider,#quota-panel .qc-provider:hover{background:#155646!important;color:#d8f5e7!important}
#quota-panel .mask{background:#071918!important;color:#9ccdbb!important;border-color:#286a5a!important}
#quota-panel .qc-overlay{background:rgba(0,10,8,.74)!important}
#quota-panel .bar{background:#1a3b34!important}
#quota-panel .fill{background:#42d89e!important}
#quota-panel .error{color:#f1b46c!important}
#quota-panel .status{color:#42d89e!important}
#quota-panel .status.warn{color:#f1b46c!important}
#quota-panel .status.danger{color:#f08383!important}
</style></head><body id="quota-panel"><div class="app"><aside class="nav"><div class="logo">额度中心</div><nav class="nav-group"><a class="nav-item {{if eq .View "overview"}}active{{end}}" href="?view=overview">总览<small>多平台额度</small></a><a class="nav-item {{if eq .View "accounts"}}active{{end}}" href="?view=accounts">账号配置<small>Token &amp; Tokens</small></a></nav></aside><main class="main">
<div class="header"><div><h1>{{if eq .View "accounts"}}账号配置{{else}}额度总览{{end}}</h1><p>{{if eq .View "accounts"}}选择供应商并管理多个 Key / Token{{else}}多供应商额度、窗口与重置时间{{end}}</p></div><button class="action" type="button" data-open-add>+ 添加连接</button></div>
<div class="meta"><span>{{len .Plans}} 个连接</span><span>最后更新 <time datetime="{{.GeneratedAt.UTC.Format "2006-01-02T15:04:05Z"}}" data-local-time data-local-seconds>{{.GeneratedAt.UTC.Format "2006-01-02 15:04:05"}}</time></span><span>桌面端四列</span></div>
{{if .Plans}}<section class="grid">{{range .Plans}}<article class="card" data-account-id="{{.ID}}"><div class="card-head"><div><div class="card-title">{{accountBrand .}} · {{.Label}}</div><div class="card-sub">{{if .Plan}}{{.Plan}}{{else}}账号连接{{end}}{{if eq .Source "cpa"}} · CPA 认证{{end}}</div></div>{{if ne $.View "accounts"}}<button class="refresh" type="button" data-refresh="{{.ID}}" aria-label="刷新 {{.Label}}">↻ refresh</button>{{end}}</div>{{if eq $.View "accounts"}}<div class="status">已连接</div><div class="mask">{{if .CredentialMask}}{{.CredentialMask}}{{else}}••••••••{{end}}</div>{{if ne .Source "cpa"}}<div class="card-actions"><button class="refresh" type="button" data-edit="{{.ID}}" data-provider="{{.Provider}}" data-label="{{.Label}}">编辑</button><button class="refresh" type="button" data-delete="{{.ID}}">删除</button></div>{{end}}{{else if .Quota}}{{range windowGroups .Quota}}<div class="window-group">{{if .Label}}<div class="window-group-title">{{.Label}}</div>{{end}}{{range .Windows}}<div class="window"><div class="window-head"><span>{{.Name}}</span><span class="remaining">{{remainingText .}}</span></div><div class="bar"><div class="fill" style="width:{{printf "%.0f" .RemainingPercent}}%"></div></div>{{if .ResetAt}}<div class="reset"><time datetime="{{.ResetAt.UTC.Format "2006-01-02T15:04:05Z"}}" data-relative-reset>{{.ResetAt.UTC.Format "2006-01-02 15:04"}}</time></div>{{end}}</div>{{end}}</div>{{end}}{{if not (windows .Quota)}}<div class="status">已连接，暂无窗口数据</div>{{end}}{{else if .Error}}<div class="error">{{.Error}}</div>{{else}}<div class="status">等待刷新</div>{{end}}</article>{{end}}</section>{{else}}<div class="empty">暂无连接。点击“添加连接”开始配置智谱、MiniMax、方舟，或同步 CPA 已登录账号。</div>{{end}}
</main></div><div class="qc-overlay" data-add-modal hidden><section class="qc-modal" role="dialog" aria-modal="true" aria-label="添加供应商连接"><div class="qc-modal-head"><div><h3 data-modal-title>添加供应商连接</h3><p>手动添加智谱、MiniMax 或方舟；Grok、Codex、Gemini 用下方同步即可。</p></div><button class="qc-close" type="button" data-close-add aria-label="关闭">×</button></div><form class="qc-form" data-add-form><input type="hidden" name="account_id" id="account-id" value=""><div class="qc-field"><label>CPA 原生账号</label><button class="qc-cancel" type="button" data-reload-accounts>同步 CPA 登录账号</button><small>Grok、Codex、Gemini 已在 CPA 登录时点此同步，无需在此重复添加。</small></div><div class="qc-field"><label>供应商</label><div class="qc-provider-list" role="radiogroup" aria-label="供应商">{{range $index, $provider := providers}}<input class="qc-provider-input" type="radio" id="provider-{{$provider.ID}}" name="provider" value="{{$provider.ID}}" data-provider="{{$provider.ID}}" {{if eq $index 0}}checked{{end}}><label class="qc-provider" for="provider-{{$provider.ID}}">{{$provider.Label}}</label>{{end}}</div><small>选择后会自动匹配额度接口和凭据类型。</small></div><div class="qc-field"><label for="plan">套餐 / 账号类型</label><select class="qc-select" id="plan" name="plan"><option value="api-Key">智谱 · API Key</option></select></div><div class="qc-field"><label for="label">显示名称</label><input class="qc-input" id="label" name="label" placeholder="例如：工作账号" required></div><div class="qc-field"><label for="credential">API Key / Token</label><input class="qc-input" id="credential" name="credential" type="password" placeholder="保存后默认遮罩"><small>验证请求成功后才写入账号；实时额度不可用时会显示原因。</small></div><div class="qc-field qc-ark-only" hidden><label for="access-id">方舟 AccessKey ID</label><input class="qc-input" id="access-id" name="access_id" autocomplete="off"></div><div class="qc-field qc-ark-only" hidden><label for="secret">方舟 Secret AccessKey</label><input class="qc-input" id="secret" name="secret" type="password" autocomplete="off"></div><p class="qc-note">安全提示：卡片、日志和状态 JSON 不回显明文 Key；编辑时需要重新输入。</p><div class="qc-actions"><button class="qc-cancel" type="button" data-close-add>取消</button><button class="qc-submit" type="submit" name="submit">验证并添加</button></div></form></section></div><script>
(()=>{const formatLocalDateTime=(iso,withSeconds)=>{const date=new Date(iso);if(Number.isNaN(date.getTime()))return '';const options={year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false,timeZoneName:'short'};if(withSeconds)options.second='2-digit';return date.toLocaleString(undefined,options)};const formatRelativeReset=(iso)=>{const date=new Date(iso);if(Number.isNaN(date.getTime()))return '';const minutes=Math.max(0,Math.round((date.getTime()-Date.now())/60000));const days=Math.floor(minutes/1440);const hours=Math.floor((minutes%1440)/60);const mins=minutes%60;const parts=[];if(days)parts.push(days+'天');if(hours)parts.push(hours+'小时');if(mins||!parts.length)parts.push(mins+'分钟');return parts.join('')+'后刷新'};document.querySelectorAll('time[data-local-time]').forEach(node=>{const text=formatLocalDateTime(node.dateTime||node.getAttribute('datetime'),node.hasAttribute('data-local-seconds'));if(text)node.textContent=text});document.querySelectorAll('time[data-relative-reset]').forEach(node=>{const text=formatRelativeReset(node.dateTime||node.getAttribute('datetime'));if(text)node.textContent=text});const modal=document.querySelector('[data-add-modal]');const form=document.querySelector('[data-add-form]');const syncButton=document.querySelector('[data-reload-accounts]');const open=()=>modal.hidden=false;const close=()=>modal.hidden=true;const PANEL_ENC_PREFIX='enc::v1::';const PANEL_SECRET_SALT='cli-proxy-api-webui::secure-storage';const tryParseJSON=text=>{try{return JSON.parse(text)}catch{return null}};const panelKeyBytes=()=>{try{return new TextEncoder().encode(PANEL_SECRET_SALT+'|'+window.location.host+'|'+navigator.userAgent)}catch{return new TextEncoder().encode(PANEL_SECRET_SALT)}};const deobfuscatePanelValue=payload=>{const raw=String(payload==null?'':payload);if(!raw||!raw.startsWith(PANEL_ENC_PREFIX))return raw;try{const binary=atob(raw.slice(PANEL_ENC_PREFIX.length));const encrypted=Uint8Array.from(binary,char=>char.charCodeAt(0));const key=panelKeyBytes();const plain=encrypted.map((byte,index)=>byte^key[index%key.length]);return new TextDecoder().decode(plain)}catch{return raw}};const storageCandidates=()=>{const stores=[];try{stores.push(window.localStorage)}catch{}try{stores.push(window.sessionStorage)}catch{}try{if(window.parent&&window.parent!==window){try{stores.push(window.parent.localStorage)}catch{}try{stores.push(window.parent.sessionStorage)}catch{}}}catch{}return stores};const readStorageItem=(store,name)=>{try{return store&&store.getItem?store.getItem(name):null}catch{return null}};const managementKey=()=>{for(const store of storageCandidates()){const authRaw=readStorageItem(store,'cli-proxy-auth');if(!authRaw)continue;const parsed=tryParseJSON(deobfuscatePanelValue(authRaw));const key=parsed?.state?.managementKey||parsed?.managementKey||'';if(String(key).trim())return String(key).trim()}return''};document.querySelectorAll('[data-close-add]').forEach(button=>button.addEventListener('click',close));const loadManagementPanel=async(view)=>{const key=managementKey();const url=new URL(location.href);url.pathname='/v0/management/plugins/{{.PluginID}}/status';if(typeof view==='string'&&view)url.searchParams.set('view',view);if(!key){alert('未能从 CPA 管理面板读取管理授权，请重新进入管理面板或手动保存。');return}if(syncButton){syncButton.disabled=true;syncButton.textContent='同步中…'}try{const response=await fetch(url,{headers:{Authorization:'Bearer '+key}});const html=await response.text();if(!response.ok)throw new Error('HTTP '+response.status);document.open();document.write(html);document.close()}catch(error){if(syncButton){syncButton.disabled=false;syncButton.textContent='同步 CPA 登录账号'}alert('同步 CPA 登录失败：'+error.message)}};document.querySelectorAll('[data-reload-accounts]').forEach(button=>button.addEventListener('click',loadManagementPanel));document.querySelectorAll('[data-refresh]').forEach(button=>button.addEventListener('click',()=>{button.disabled=true;button.textContent='↻…';loadManagementPanel()}));if(form){const providerInputs=[...form.querySelectorAll('input[name="provider"]')];const arkFields=form.querySelectorAll('.qc-ark-only');const plan=form.elements.plan;const label=form.elements.label;const credential=form.elements.credential;const credentialField=credential&&credential.closest('.qc-field');const submit=form.elements.submit;const presets={zhipu:{plan:'api-Key',planLabel:'智谱 · API Key',label:'智谱主账号',placeholder:'粘贴智谱 API Key，保存后默认遮罩'},minimax:{plan:'coding-plan',planLabel:'MiniMax · Coding Plan',label:'MiniMax 主账号',placeholder:'粘贴 MiniMax API Key，保存后默认遮罩'},'opencode-go':{plan:'go',planLabel:'OpenCode Go',label:'OpenCode Go 主账号',placeholder:'粘贴 OpenCode Go Key，保存后默认遮罩'},ark:{plan:'agent-plan',planLabel:'方舟 · Agent Plan',label:'方舟主账号',placeholder:'方舟使用下方 AccessKey ID / Secret'}};let autoLabel='';const updateProvider=provider=>{const preset=presets[provider]||presets.zhipu;arkFields.forEach(field=>field.hidden=provider!=='ark');if(credentialField){credentialField.hidden=provider==='ark'}if(plan){plan.replaceChildren(new Option(preset.planLabel,preset.plan));plan.value=preset.plan}if(label&&(!label.value.trim()||label.value===autoLabel)){label.value=preset.label;autoLabel=preset.label}if(credential){credential.placeholder=preset.placeholder;if(provider==='ark')credential.value=''}if(submit){submit.disabled=false;submit.textContent='验证并添加'}};providerInputs.forEach(input=>input.addEventListener('change',()=>updateProvider(input.value)));updateProvider(form.elements.provider.value||'zhipu');const modalTitle=document.querySelector('[data-modal-title]');const accountIdInput=form.elements.account_id;const setModalMode=(mode,preset)=>{if(modalTitle)modalTitle.textContent=mode==='edit'?'编辑供应商连接':'添加供应商连接';if(accountIdInput)accountIdInput.value=preset&&preset.id||'';providerInputs.forEach(input=>{input.disabled=mode==='edit';if(preset&&input.value===preset.provider)input.checked=true;});updateProvider((preset&&preset.provider)||form.elements.provider.value||'zhipu');if(label&&preset&&preset.label)label.value=preset.label;if(credential)credential.value='';if(form.elements.access_id)form.elements.access_id.value='';if(form.elements.secret)form.elements.secret.value='';};document.querySelectorAll('[data-open-add]').forEach(button=>button.addEventListener('click',()=>{setModalMode('add');open();}));document.querySelectorAll('[data-edit]').forEach(button=>button.addEventListener('click',()=>{setModalMode('edit',{id:button.getAttribute('data-edit')||'',provider:button.getAttribute('data-provider')||'zhipu',label:button.getAttribute('data-label')||''});open();}));document.querySelectorAll('[data-delete]').forEach(button=>button.addEventListener('click',async()=>{const id=button.getAttribute('data-delete')||'';if(!id||!confirm('确定删除这个连接？删除后需要重新添加凭据。'))return;const key=managementKey();if(!key){alert('CPA 管理授权未传入插件窗口，无法删除。');return}const headers={Authorization:'Bearer '+key};button.disabled=true;button.textContent='删除中…';try{let response=await fetch('/v0/management/plugins/{{.PluginID}}/accounts?id='+encodeURIComponent(id),{method:'DELETE',headers});if(response.status===401){alert('CPA 管理授权未传入插件窗口，无法删除。');return}if(!response.ok){let detail='删除失败，请检查管理权限';try{const data=await response.json();if(data&&data.error)detail=String(data.error)}catch{}alert(detail);return}await loadManagementPanel('accounts')}finally{button.disabled=false;button.textContent='删除'}}));form.addEventListener('submit',async event=>{event.preventDefault();const payload={id:form.elements.account_id?.value||'',provider:form.elements.provider.value,plan:form.elements.plan.value,label:form.elements.label.value,credential:form.elements.credential.value,access_id:form.elements.access_id?.value||form.elements.access_key_id?.value||'',secret:form.elements.secret?.value||form.elements.secret_access_key?.value||''};const key=managementKey();if(submit){submit.disabled=true;submit.textContent='验证中…'}try{const response=await fetch('/v0/management/plugins/{{.PluginID}}/accounts',{method:'POST',headers:{'Content-Type':'application/json',...(key?{Authorization:'Bearer '+key}:{})},body:JSON.stringify(payload)});if(response.status===401){alert('CPA 管理授权未传入插件窗口，无法保存；Grok/Codex/Gemini 已登录 CPA 时无需重复保存。');return}if(!response.ok){let detail='保存失败，请检查凭据或管理权限';try{const data=await response.json();if(data&&data.error)detail=String(data.error)}catch{}alert(detail);return}await loadManagementPanel('accounts')}finally{if(submit){submit.disabled=false;submit.textContent='验证并添加'}}})}})();
</script></body></html>`))

type providerOption struct {
	ID    string
	Label string
}

var providerOptions = []providerOption{
	{ID: ProviderZhipu, Label: "智谱"},
	{ID: ProviderMiniMax, Label: "MiniMax"},
	{ID: ProviderOpenCode, Label: "OpenCode Go"},
	{ID: ProviderArk, Label: "方舟"},
}

func displayWindows(quota *QuotaResult) []QuotaWindow {
	if quota == nil {
		return nil
	}
	if len(quota.Windows) > 0 {
		return quota.Windows
	}
	windows := make([]QuotaWindow, 0, 4)
	for _, candidate := range []QuotaWindow{quota.FiveHour, quota.Weekly, quota.Monthly, quota.MCP} {
		if candidate.Name != "" || candidate.UsedPercent != 0 || candidate.RemainingPercent != 0 || candidate.ResetAt != nil {
			windows = append(windows, candidate)
		}
	}
	return windows
}

type quotaWindowGroup struct {
	Label   string
	Windows []QuotaWindow
}

func displayWindowGroups(quota *QuotaResult) []quotaWindowGroup {
	windows := displayWindows(quota)
	if len(windows) == 0 {
		return nil
	}
	groups := make([]quotaWindowGroup, 0, 2)
	for _, window := range windows {
		if len(groups) > 0 && groups[len(groups)-1].Label == window.Group {
			groups[len(groups)-1].Windows = append(groups[len(groups)-1].Windows, window)
			continue
		}
		groups = append(groups, quotaWindowGroup{Label: window.Group, Windows: []QuotaWindow{window}})
	}
	return groups
}

func remainingText(window QuotaWindow) string {
	if window.Available {
		return "额度可用"
	}
	return formatPercent(window.RemainingPercent) + " 剩余"
}

func accountBrand(result PlanResult) string {
	if strings.Contains(strings.ToLower(result.AuthType), "antigravity") {
		return "Antigravity"
	}
	return providerLabel(result.Provider)
}

func formatPercent(value float64) string {
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return formatFloat(value) + "%"
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64)
}

func maskCredential(value string) string {
	_ = strings.TrimSpace(value)
	return "••••••••"
}

func RenderPanel(results []PlanResult, generatedAt time.Time) string {
	return RenderPanelView(results, generatedAt, "overview")
}

func RenderPanelView(results []PlanResult, generatedAt time.Time, view string) string {
	if view != "accounts" {
		view = "overview"
	}
	var output strings.Builder
	data := panelData{GeneratedAt: generatedAt, Plans: results, View: view, PluginID: pluginID}
	if err := panelTemplate.Execute(&output, data); err != nil {
		return "<!doctype html><title>额度中心</title><p>quota panel unavailable: " + template.HTMLEscapeString(err.Error()) + "</p>"
	}
	return output.String()
}

type statusPayload struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Plans       []PlanResult `json:"plans"`
}

func RenderStatusJSON(results []PlanResult) []byte {
	if len(results) > 0 && !results[0].FetchedAt.IsZero() {
		return mustJSON(statusPayload{GeneratedAt: results[0].FetchedAt, Plans: results})
	}
	return mustJSON(statusPayload{GeneratedAt: time.Now().UTC(), Plans: results})
}
