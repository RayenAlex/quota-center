package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFileAccountStoreDoesNotCreateCPAAuthJSON(t *testing.T) {
	dir := t.TempDir()
	store := &fileAccountStore{dir: dir}
	credential, _ := json.Marshal(map[string]any{
		"id":       "zhipu",
		"provider": "zhipu",
		"type":     "zhipu",
		"plan":     "api-Key",
		"label":    "智谱主账号",
		"api_key":  "secret-key",
	})
	if _, err := store.Save(context.Background(), "zhipu.json", credential); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "zhipu.json")); !os.IsNotExist(err) {
		t.Fatal("plugin account must not be written as a CPA auth JSON file")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != pluginAccountsFileName {
		t.Fatalf("auth dir entries = %v, want only %s", names(entries), pluginAccountsFileName)
	}
	listed, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "zhipu.json" {
		t.Fatalf("List() = %#v", listed)
	}
	var got map[string]string
	if err := json.Unmarshal(listed[0].JSON, &got); err != nil {
		t.Fatalf("decode stored credential: %v", err)
	}
	if got["api_key"] != "secret-key" || got["id"] != "zhipu" {
		t.Fatalf("stored credential = %#v", got)
	}
}

func TestFileAccountStorePromotesLegacyAccountsFile(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, legacyPluginAccountsFileName)
	payload := []byte(`{"accounts":[{"id":"zhipu","provider":"zhipu","type":"zhipu","plan":"api-Key","label":"智谱主账号","api_key":"secret-key"}]}`)
	if err := os.WriteFile(legacy, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	store := &fileAccountStore{dir: dir}
	listed, err := store.List(context.Background())
	if err != nil || len(listed) != 1 || listed[0].Name != "zhipu.json" {
		t.Fatalf("List() = %#v err=%v", listed, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy accounts file must be renamed")
	}
	if _, err := os.Stat(filepath.Join(dir, pluginAccountsFileName)); err != nil {
		t.Fatalf("promoted accounts file missing: %v", err)
	}
}

func TestFileAccountStoreConcurrentSavesPreserveAllAccounts(t *testing.T) {
	store := &fileAccountStore{dir: t.TempDir()}
	const accounts = 24

	var wg sync.WaitGroup
	errs := make(chan error, accounts)
	for i := 0; i < accounts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "account-" + string(rune('a'+i))
			credential, _ := json.Marshal(map[string]any{
				"id":       id,
				"provider": "zhipu",
				"type":     "zhipu",
				"label":    id,
				"api_key":  "secret-" + id,
			})
			if _, err := store.Save(context.Background(), id+".json", credential); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != accounts {
		t.Fatalf("len(List()) = %d, want %d", len(entries), accounts)
	}
}

func TestFileAccountStoreSaveDoesNotFollowPredictableTempSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim.json")
	const original = "must not be overwritten"
	if err := os.WriteFile(victim, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, pluginAccountsFileName+".tmp")); err != nil {
		t.Fatal(err)
	}

	store := &fileAccountStore{dir: dir}
	credential := []byte(`{"id":"zhipu","provider":"zhipu","type":"zhipu","api_key":"secret-key"}`)
	if _, err := store.Save(context.Background(), "zhipu.json", credential); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("predictable temp symlink overwrote victim: %q", got)
	}
}

func TestMigratePluginOwnedAuthFilesLeavesNativeCPAFiles(t *testing.T) {
	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "zhipu.json")
	nativePath := filepath.Join(dir, "codex-user.json")
	if err := os.WriteFile(pluginPath, []byte(`{"id":"zhipu","provider":"zhipu","type":"zhipu","plan":"api-Key","label":"智谱主账号","api_key":"secret-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nativePath, []byte(`{"type":"codex","access_token":"secret-codex-token","account_id":"acct-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	host := &recordingAuthStore{entries: []StoredCredential{
		{Name: "zhipu.json", Path: pluginPath, Provider: ProviderZhipu, JSON: json.RawMessage(`{"id":"zhipu","provider":"zhipu","type":"zhipu","plan":"api-Key","label":"智谱主账号","api_key":"secret-key"}`)},
		{AuthIndex: "codex-index", Name: "codex-user.json", Path: nativePath, Provider: ProviderCodex, JSON: json.RawMessage(`{"type":"codex","access_token":"secret-codex-token","account_id":"acct-1"}`)},
	}}
	accounts := &fileAccountStore{dir: dir}
	if err := migratePluginOwnedAuthFiles(context.Background(), host, accounts); err != nil {
		t.Fatalf("migratePluginOwnedAuthFiles() error = %v", err)
	}
	if _, err := os.Stat(pluginPath); !os.IsNotExist(err) {
		t.Fatal("plugin-owned CPA auth JSON should be removed after migration")
	}
	if _, err := os.Stat(nativePath); err != nil {
		t.Fatalf("native CPA auth file missing: %v", err)
	}
	if len(host.saved) != 0 {
		t.Fatalf("host writes = %#v, want none", host.saved)
	}
	listed, err := accounts.List(context.Background())
	if err != nil || len(listed) != 1 || listed[0].Name != "zhipu.json" {
		t.Fatalf("migrated accounts = %#v err=%v", listed, err)
	}
}

func TestMigratePluginOwnedAuthFilesDoesNotRequireHostDelete(t *testing.T) {
	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "zhipu.json")
	payload := []byte(`{"id":"zhipu","provider":"zhipu","type":"zhipu","api_key":"secret-key"}`)
	if err := os.WriteFile(pluginPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	host := &recordingAuthStore{
		entries: []StoredCredential{{Name: "zhipu.json", Path: pluginPath, Provider: ProviderZhipu, JSON: json.RawMessage(payload)}},
	}
	accounts := &fileAccountStore{dir: dir}

	if err := migratePluginOwnedAuthFiles(context.Background(), host, accounts); err != nil {
		t.Fatalf("migratePluginOwnedAuthFiles() error = %v", err)
	}
	if _, err := os.Stat(pluginPath); !os.IsNotExist(err) {
		t.Fatalf("legacy plugin file should be removed directly: stat err=%v", err)
	}
	listed, err := accounts.List(context.Background())
	if err != nil || len(listed) != 1 || listed[0].Name != "zhipu.json" {
		t.Fatalf("migrated accounts = %#v err=%v", listed, err)
	}
}

func TestMigratePluginOwnedAuthFilesDoesNotRemoveOutsidePluginStore(t *testing.T) {
	storageDir := t.TempDir()
	legacyDir := t.TempDir()
	legacyPath := filepath.Join(legacyDir, "zhipu.json")
	payload := []byte(`{"id":"zhipu","provider":"zhipu","type":"zhipu","api_key":"secret-key"}`)
	if err := os.WriteFile(legacyPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	host := &recordingAuthStore{
		entries: []StoredCredential{{Name: "zhipu.json", Path: legacyPath, Provider: ProviderZhipu, JSON: json.RawMessage(payload)}},
	}
	accounts := &fileAccountStore{dir: storageDir}

	if err := migratePluginOwnedAuthFiles(context.Background(), host, accounts); err != nil {
		t.Fatalf("migratePluginOwnedAuthFiles() error = %v", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("outside auth file must be preserved: %v", err)
	}
	listed, err := accounts.List(context.Background())
	if err != nil || len(listed) != 1 || listed[0].Name != "zhipu.json" {
		t.Fatalf("migrated accounts = %#v err=%v", listed, err)
	}
}

func TestIsPluginManagedProvider(t *testing.T) {
	if !isPluginManagedProvider(ProviderZhipu) || !isPluginManagedProvider(ProviderArk) || !isPluginManagedProvider(ProviderMiniMax) || !isPluginManagedProvider(ProviderOpenCode) {
		t.Fatal("manual providers must be plugin-managed")
	}
	if isPluginManagedProvider(ProviderCodex) || isPluginManagedProvider(ProviderGemini) || isPluginManagedProvider(ProviderGrok) {
		t.Fatal("CPA native providers must stay in CPA auth files")
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}

type recordingAuthSave struct {
	name string
	body []byte
}

type recordingAuthStore struct {
	entries []StoredCredential
	saved   []recordingAuthSave
}

func (s *recordingAuthStore) List(context.Context) ([]StoredCredential, error) {
	return append([]StoredCredential(nil), s.entries...), nil
}

func (s *recordingAuthStore) Get(_ context.Context, authIndex string) (StoredCredential, error) {
	for _, entry := range s.entries {
		if entry.AuthIndex == authIndex || entry.Name == authIndex || strings.TrimSuffix(entry.Name, ".json") == authIndex {
			return entry, nil
		}
	}
	return StoredCredential{}, os.ErrNotExist
}

func (s *recordingAuthStore) Save(_ context.Context, name string, credential []byte) (StoredCredential, error) {
	s.saved = append(s.saved, recordingAuthSave{name: name, body: append([]byte(nil), credential...)})
	return StoredCredential{Name: name}, nil
}
