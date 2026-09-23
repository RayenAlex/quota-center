package main

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Exercise the exact browser-side auth bridge emitted by RenderPanel, not a
// reimplementation of its decoder in Go. Node is optional for Go-only builds.
func TestPanelManagementAuthStorageCompatibility(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required to exercise browser-side management auth")
	}
	html := RenderPanel(nil, time.Unix(100, 0).UTC())
	start := strings.Index(html, "const PANEL_ENC_PREFIX_V1=")
	if start < 0 {
		t.Fatal("cannot locate the panel management auth bridge start")
	}
	endOffset := strings.Index(html[start:], ";document.querySelectorAll('[data-close-add]')")
	if endOffset < 0 {
		t.Fatal("cannot locate the panel management auth bridge end")
	}
	end := start + endOffset
	bridge := html[start:end]
	const js = `
const vm = require('node:vm');
const {TextEncoder, TextDecoder} = require('node:util');
const bridge = process.argv[1];
const host = '192.168.100.100:18317';
const userAgent = 'test browser';
const salt = 'cli-proxy-api-webui::secure-storage';
const key = 'fixture-management-key';
const json = JSON.stringify({state: {managementKey: key}});
function encode(text, version) {
  const seed = new TextEncoder().encode(version === 2 ? salt+'|v2|'+host : salt+'|'+host+'|'+userAgent);
  const bytes = new TextEncoder().encode(text);
  return 'enc::v'+version+'::'+Buffer.from(bytes.map((byte,i)=>byte^seed[i%seed.length])).toString('base64');
}
function read(value) {
  const store = {getItem: name => name === 'cli-proxy-auth' ? value : null};
  const window = {location: {host}, localStorage: store, sessionStorage: store};
  window.parent = window;
  const context = {window, navigator: {userAgent}, TextEncoder, TextDecoder, atob};
  return vm.runInNewContext(bridge+';managementKey()', context);
}
for (const [name, value, expected] of [
  ['v2', encode(json, 2), key],
  ['v1', encode(json, 1), key],
  ['plaintext', json, key],
  ['unknown-field', encode(JSON.stringify({password: key}), 2), ''],
  ['malformed', 'enc::v2::broken', ''],
]) {
  const actual = read(value);
  if (actual !== expected) throw new Error(name+': expected '+JSON.stringify(expected)+', got '+JSON.stringify(actual));
}
`
	out, err := exec.Command("node", "-e", js, bridge).CombinedOutput()
	if err != nil {
		t.Fatalf("management auth storage compatibility: %v\n%s", err, out)
	}
}
