package langgraphcompat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredGatewayModelsFromJSONPreservesInputOrder(t *testing.T) {
	t.Setenv("DEERFLOW_MODELS_JSON", `[
		{"name":"zeta","model":"provider/zeta"},
		{"name":"alpha","model":"provider/alpha"},
		{"name":"zeta","model":"provider/zeta-duplicate"}
	]`)

	models := configuredGatewayModels("fallback-model")
	if len(models) != 2 {
		t.Fatalf("models=%d want=2", len(models))
	}
	if models[0].Name != "zeta" || models[1].Name != "alpha" {
		t.Fatalf("order=%q,%q want zeta,alpha", models[0].Name, models[1].Name)
	}
	if models[0].Model != "provider/zeta" {
		t.Fatalf("first duplicate should win, got %q", models[0].Model)
	}
}

func TestConfiguredGatewayModelsFromListPreservesInputOrder(t *testing.T) {
	t.Setenv("DEERFLOW_MODELS", "zeta=provider/zeta, alpha=provider/alpha, zeta=provider/zeta-duplicate")

	models := configuredGatewayModels("fallback-model")
	if len(models) != 2 {
		t.Fatalf("models=%d want=2", len(models))
	}
	if models[0].Name != "zeta" || models[1].Name != "alpha" {
		t.Fatalf("order=%q,%q want zeta,alpha", models[0].Name, models[1].Name)
	}
	if models[0].Model != "provider/zeta" {
		t.Fatalf("first duplicate should win, got %q", models[0].Model)
	}
}

func TestConfiguredGatewayModelsFromConfigPreservesInputOrder(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
models:
  - name: zeta
    model: provider/zeta
  - name: alpha
    model: provider/alpha
  - name: zeta
    model: provider/zeta-duplicate
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("DEERFLOW_CONFIG_PATH", configPath)

	models := configuredGatewayModels("fallback-model")
	if len(models) != 2 {
		t.Fatalf("models=%d want=2", len(models))
	}
	if models[0].Name != "zeta" || models[1].Name != "alpha" {
		t.Fatalf("order=%q,%q want zeta,alpha", models[0].Name, models[1].Name)
	}
	if models[0].Model != "provider/zeta" {
		t.Fatalf("first duplicate should win, got %q", models[0].Model)
	}
}

func TestConfiguredGatewayModelsFromLegacyConfigEnvPath(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
models:
  - name: legacy
    model: provider/legacy
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("DEER_FLOW_CONFIG_PATH", configPath)

	models := configuredGatewayModels("fallback-model")
	if len(models) != 1 {
		t.Fatalf("models=%d want=1", len(models))
	}
	if models[0].Name != "legacy" || models[0].Model != "provider/legacy" {
		t.Fatalf("models=%#v", models)
	}
}
