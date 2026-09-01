package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type valuesTestConfig struct {
	OriginalField string `mapstructure:"original_field"`
	Enabled       bool   `mapstructure:"enabled"`
	Workers       int    `mapstructure:"workers"`
	FromEnv       string `mapstructure:"from_env"`
	FromDefault   string `mapstructure:"from_default"`
}

func TestLoadEcoConfigRemainsBackwardCompatible(t *testing.T) {
	t.Setenv("PROFILE_TEST_FROM_ENV", "from-environment")
	configPath := writeTestConfig(t, `
original_field: ${PROFILE_TEST_ORIGINAL_FIELD:default}
enabled: ${PROFILE_TEST_ENABLED:true}
workers: ${PROFILE_TEST_WORKERS:3}
from_env: ${PROFILE_TEST_FROM_ENV:default}
from_default: ${PROFILE_TEST_NOT_SET:from-default}
`)

	legacy, err := LoadEcoConfig[valuesTestConfig](configPath)
	if err != nil {
		t.Fatal(err)
	}
	withNilValues, err := LoadEcoConfigWithValues[valuesTestConfig](configPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(legacy, withNilValues) {
		t.Fatalf("legacy API behavior changed: legacy=%#v new=%#v", legacy, withNilValues)
	}
}

func TestLoadEcoConfigWithValues(t *testing.T) {
	const externalName = "PROFILE_TEST_ORIGINAL_FIELD"
	t.Setenv(externalName, "environment-must-not-win")
	t.Setenv("PROFILE_TEST_FROM_ENV", "from-environment")

	configPath := writeTestConfig(t, `
original_field: ${PROFILE_TEST_ORIGINAL_FIELD:default}
enabled: ${PROFILE_TEST_ENABLED:false}
workers: ${PROFILE_TEST_WORKERS:1}
from_env: ${PROFILE_TEST_FROM_ENV:default}
from_default: ${PROFILE_TEST_NOT_SET:from-default}
`)

	config, err := LoadEcoConfigWithValues[valuesTestConfig](configPath, map[string]any{
		externalName:           "from-values",
		"PROFILE_TEST_ENABLED": true,
		"PROFILE_TEST_WORKERS": float64(4),
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.OriginalField != "from-values" || !config.Enabled || config.Workers != 4 {
		t.Fatalf("external values were not applied: %#v", config)
	}
	if config.FromEnv != "from-environment" || config.FromDefault != "from-default" {
		t.Fatalf("environment/default precedence changed: %#v", config)
	}
	if got := os.Getenv(externalName); got != "environment-must-not-win" {
		t.Fatalf("LoadEcoConfigWithValues mutated process environment: %q", got)
	}
}

func TestLoadEcoConfigWithValuesRejectsComplexValueWithoutLeakingIt(t *testing.T) {
	const sensitive = "must-not-appear"
	configPath := writeTestConfig(t, `value: ${PROFILE_TEST_COMPLEX:default}`)

	_, err := LoadEcoConfigWithValues[map[string]any](configPath, map[string]any{
		"PROFILE_TEST_COMPLEX": map[string]any{"secret": sensitive},
	})
	if err == nil {
		t.Fatal("expected complex value to be rejected")
	}
	if strings.Contains(err.Error(), sensitive) {
		t.Fatal("error leaked config value")
	}
}

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "ecosystem.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath
}
