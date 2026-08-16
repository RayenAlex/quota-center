package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	pluginAccountsFileName       = ".quota-center-accounts"
	legacyPluginAccountsFileName = ".zhipu-quota-accounts"
)

type fileAccountStore struct {
	mu   sync.Mutex
	dir  string
	host AuthStore
}

type pluginAccountFile struct {
	Accounts []json.RawMessage `json:"accounts"`
}

func isPluginManagedProvider(provider Provider) bool {
	switch provider {
	case ProviderZhipu, ProviderMiniMax, ProviderOpenCode, ProviderArk:
		return true
	default:
		return false
	}
}

func (s *fileAccountStore) List(ctx context.Context) ([]StoredCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, records, err := s.load(ctx)
	if err != nil {
		return nil, nil
	}
	entries := make([]StoredCredential, 0, len(records))
	for _, record := range records {
		entry, err := storedCredentialFromPluginRecord(record)
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *fileAccountStore) Get(ctx context.Context, authIndex string) (StoredCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := strings.TrimSuffix(strings.TrimSpace(authIndex), ".json")
	_, records, err := s.load(ctx)
	if err != nil {
		return StoredCredential{}, err
	}
	for _, record := range records {
		entry, err := storedCredentialFromPluginRecord(record)
		if err != nil {
			continue
		}
		if strings.TrimSuffix(entry.Name, ".json") == want || entry.AuthIndex == want {
			return entry, nil
		}
	}
	return StoredCredential{}, fmt.Errorf("account %q not found", authIndex)
}

func (s *fileAccountStore) Save(ctx context.Context, name string, credential []byte) (StoredCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := strings.TrimSuffix(strings.TrimSpace(name), ".json")
	if id == "" || !planIDPattern.MatchString(id) {
		return StoredCredential{}, fmt.Errorf("account id is invalid")
	}
	var payload map[string]any
	if err := json.Unmarshal(credential, &payload); err != nil {
		return StoredCredential{}, fmt.Errorf("invalid account payload")
	}
	if strings.TrimSpace(fmt.Sprint(payload["id"])) == "" {
		payload["id"] = id
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		return StoredCredential{}, err
	}
	path, records, err := s.load(ctx)
	if err != nil {
		return StoredCredential{}, err
	}
	next := make([]json.RawMessage, 0, len(records)+1)
	replaced := false
	for _, record := range records {
		existing, err := storedCredentialFromPluginRecord(record)
		if err != nil {
			continue
		}
		if strings.TrimSuffix(existing.Name, ".json") == id {
			next = append(next, json.RawMessage(append([]byte(nil), normalized...)))
			replaced = true
			continue
		}
		next = append(next, record)
	}
	if !replaced {
		next = append(next, json.RawMessage(append([]byte(nil), normalized...)))
	}
	if err := s.write(path, next); err != nil {
		return StoredCredential{}, err
	}
	return storedCredentialFromPluginRecord(normalized)
}

func (s *fileAccountStore) Delete(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := strings.TrimSuffix(strings.TrimSpace(name), ".json")
	if id == "" {
		return fmt.Errorf("auth name is required")
	}
	path, records, err := s.load(ctx)
	if err != nil {
		return err
	}
	next := make([]json.RawMessage, 0, len(records))
	for _, record := range records {
		existing, err := storedCredentialFromPluginRecord(record)
		if err != nil {
			continue
		}
		if strings.TrimSuffix(existing.Name, ".json") == id {
			continue
		}
		next = append(next, record)
	}
	return s.write(path, next)
}

func (s *fileAccountStore) load(context.Context) (string, []json.RawMessage, error) {
	path, err := s.filePath()
	if err != nil {
		return "", nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, nil, nil
		}
		return "", nil, fmt.Errorf("read plugin accounts: %w", err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return path, nil, nil
	}
	var file pluginAccountFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return "", nil, fmt.Errorf("decode plugin accounts: %w", err)
	}
	return path, file.Accounts, nil
}

func (s *fileAccountStore) write(path string, records []json.RawMessage) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create plugin account dir: %w", err)
	}
	raw, err := json.Marshal(pluginAccountFile{Accounts: records})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create plugin account temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set plugin account temp file permissions: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write plugin accounts: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close plugin account temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace plugin accounts: %w", err)
	}
	return nil
}

func (s *fileAccountStore) removeMigratedLegacyFile(path, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	storagePath, err := s.filePath()
	if err != nil {
		return err
	}
	if !isDirectAuthFile(path, filepath.Dir(storagePath), name) {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func isDirectAuthFile(path, authDir, name string) bool {
	path = strings.TrimSpace(path)
	name = strings.TrimSpace(name)
	if path == "" || name == "" || filepath.Base(name) != name || !strings.EqualFold(filepath.Ext(path), ".json") || filepath.Base(path) != name {
		return false
	}
	resolvedAuthDir, err := filepath.EvalSymlinks(authDir)
	if err != nil {
		return false
	}
	resolvedPathDir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return false
	}
	return resolvedPathDir == resolvedAuthDir
}

func (s *fileAccountStore) filePath() (string, error) {
	if s == nil {
		return "", fmt.Errorf("account storage unavailable")
	}
	// Callers must hold s.mu; dir resolution also initializes the cached path.
	dir := strings.TrimSpace(s.dir)
	host := s.host
	if dir == "" {
		resolved, err := discoverAuthDir(host)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(s.dir) == "" {
			s.dir = resolved
		}
		dir = s.dir
	}
	if err := promoteLegacyAccountsFile(dir); err != nil {
		return "", err
	}
	return filepath.Join(dir, pluginAccountsFileName), nil
}

func promoteLegacyAccountsFile(dir string) error {
	next := filepath.Join(dir, pluginAccountsFileName)
	if _, err := os.Stat(next); err == nil {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	prev := filepath.Join(dir, legacyPluginAccountsFileName)
	if _, err := os.Stat(prev); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.Rename(prev, next); err != nil {
		return fmt.Errorf("promote legacy accounts file: %w", err)
	}
	return nil
}

func discoverAuthDir(host AuthStore) (string, error) {
	if host != nil {
		entries, err := host.List(context.Background())
		if err == nil {
			for _, entry := range entries {
				if dir := dirFromAuthPath(entry.Path); dir != "" {
					return dir, nil
				}
			}
		}
	}
	for _, candidate := range []string{
		strings.TrimSpace(os.Getenv("CLIPROXY_AUTH_DIR")),
		"/root/.cli-proxy-api",
	} {
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("auth directory is unavailable")
}

func dirFromAuthPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return ""
	}
	info, err := os.Stat(dir)
	if err == nil && info.IsDir() {
		return dir
	}
	return ""
}

func storedCredentialFromPluginRecord(raw json.RawMessage) (StoredCredential, error) {
	plan, err := accountFromStoredCredential(StoredCredential{JSON: raw})
	if err != nil {
		return StoredCredential{}, err
	}
	if plan.ID == "" {
		return StoredCredential{}, fmt.Errorf("account id is required")
	}
	return StoredCredential{
		AuthIndex: plan.ID,
		Name:      plan.ID + ".json",
		Provider:  plan.Provider,
		Label:     plan.Label,
		JSON:      append(json.RawMessage(nil), raw...),
	}, nil
}

func migratePluginOwnedAuthFiles(ctx context.Context, host, accounts AuthStore) error {
	if host == nil || accounts == nil {
		return nil
	}
	entries, err := host.List(ctx)
	if err != nil {
		return err
	}
	var first error
	for _, entry := range entries {
		detail := entry
		if entry.AuthIndex != "" {
			if got, getErr := host.Get(ctx, entry.AuthIndex); getErr == nil {
				detail = got
				if detail.Path == "" {
					detail.Path = entry.Path
				}
				if detail.Name == "" {
					detail.Name = entry.Name
				}
			}
		} else if len(entry.JSON) == 0 && entry.Name != "" {
			if got, getErr := host.Get(ctx, entry.Name); getErr == nil {
				detail = got
				if detail.Path == "" {
					detail.Path = entry.Path
				}
			}
		}
		if len(detail.JSON) == 0 {
			continue
		}
		plan, err := accountFromStoredCredential(detail)
		if err != nil || plan.Source == AuthSourceCPA || !isPluginManagedProvider(plan.Provider) {
			continue
		}
		name := firstNonEmpty(detail.Name, plan.ID+".json")
		if _, err := accounts.Save(ctx, name, detail.JSON); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if remover, ok := accounts.(interface {
			removeMigratedLegacyFile(string, string) error
		}); ok {
			if err := remover.removeMigratedLegacyFile(detail.Path, name); err != nil && first == nil {
				first = err
			}
		}
		if strings.TrimSpace(detail.Path) == "" {
			disabled, _ := json.Marshal(map[string]any{
				"id":       plan.ID,
				"type":     string(plan.Provider),
				"provider": string(plan.Provider),
				"disabled": true,
			})
			if _, err := host.Save(ctx, name, disabled); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}
