package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uinaf/autoreview/internal/protocol"
)

type Runtime struct {
	Workspace string
	root      string
	env       []string
	removeAll func(string) error
	closeMu   sync.Mutex
}

func PrepareRuntime(effective Effective, parentEnvironment []string) (*Runtime, error) {
	if err := effective.Validate(); err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp("", "autoreview-provider-")
	if err != nil {
		return nil, fmt.Errorf("create provider runtime: %w", err)
	}
	runtime := &Runtime{root: root, Workspace: filepath.Join(root, "workspace"), removeAll: os.RemoveAll}
	if err := os.Mkdir(runtime.Workspace, 0o700); err != nil {
		_ = os.RemoveAll(root)
		return nil, fmt.Errorf("create provider workspace: %w", err)
	}
	if effective.Isolation.Value == protocol.IsolationNative {
		runtime.env = append([]string(nil), parentEnvironment...)
		return runtime, nil
	}
	directories := []struct {
		name     string
		relative string
	}{
		{name: "HOME", relative: "home"},
		{name: "XDG_CONFIG_HOME", relative: "config"},
		{name: "XDG_CACHE_HOME", relative: "cache"},
		{name: "XDG_STATE_HOME", relative: "state"},
		{name: "CODEX_HOME", relative: "codex"},
		{name: "CLAUDE_CONFIG_DIR", relative: "claude"},
		{name: "CURSOR_CONFIG_DIR", relative: "cursor"},
	}
	for _, directory := range directories {
		path := filepath.Join(root, directory.relative)
		if err := os.Mkdir(path, 0o700); err != nil {
			_ = os.RemoveAll(root)
			return nil, fmt.Errorf("create strict provider state: %w", err)
		}
		runtime.env = append(runtime.env, directory.name+"="+path)
	}
	allowed := map[string]struct{}{
		"PATH": {}, "TMPDIR": {}, "TMP": {}, "TEMP": {},
		"SSL_CERT_FILE": {}, "SSL_CERT_DIR": {}, "NODE_EXTRA_CA_CERTS": {},
		"CURL_CA_BUNDLE": {}, "REQUESTS_CA_BUNDLE": {},
		"HTTP_PROXY": {}, "HTTPS_PROXY": {}, "ALL_PROXY": {}, "NO_PROXY": {},
	}
	switch effective.Engine.Value {
	case protocol.ProviderCodex:
		allowed["OPENAI_API_KEY"] = struct{}{}
	case protocol.ProviderClaude:
		allowed["ANTHROPIC_API_KEY"] = struct{}{}
	case protocol.ProviderCursor:
		allowed["CURSOR_API_KEY"] = struct{}{}
	}
	for _, entry := range parentEnvironment {
		name, _, _ := strings.Cut(entry, "=")
		if _, ok := allowed[strings.ToUpper(name)]; ok {
			runtime.env = append(runtime.env, entry)
		}
	}
	runtime.env = append(runtime.env, "LANG=C", "LC_ALL=C")
	return runtime, nil
}

func (runtime *Runtime) Environment() []string {
	return append([]string(nil), runtime.env...)
}

func (runtime *Runtime) Close() error {
	runtime.closeMu.Lock()
	defer runtime.closeMu.Unlock()
	if runtime.root == "" {
		return nil
	}
	removeAll := runtime.removeAll
	if removeAll == nil {
		removeAll = os.RemoveAll
	}
	if err := removeAll(runtime.root); err != nil {
		return fmt.Errorf("remove provider runtime: %w", err)
	}
	runtime.root = ""
	return nil
}
