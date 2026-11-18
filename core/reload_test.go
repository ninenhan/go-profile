package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"
)

type reloadTestConfig struct {
	Workers int               `mapstructure:"reload_test_workers"`
	Prompt  string            `mapstructure:"reload_test_prompt"`
	Labels  map[string]string `mapstructure:"reload_test_labels"`
	Items   []string          `mapstructure:"reload_test_items"`
}

func TestLoadEcoConfigErrorsAndRecovery(t *testing.T) {
	const valid = `reload_test_workers: 3
reload_test_prompt: ready
reload_test_labels: {owner: original}
reload_test_items: [first, second]
`
	tests := []struct {
		name    string
		content string
		env     string
		stage   string
		missing bool
	}{
		{name: "missing file", missing: true, stage: "read config"},
		{name: "invalid YAML", content: "reload_test_workers: [", stage: "parse config"},
		{name: "duplicate YAML key", content: "reload_test_workers: 1\nreload_test_workers: 2", stage: "read YAML config"},
		{name: "missing include", content: "reload_test_prompt: !include missing.txt", stage: "resolve includes"},
		{name: "incompatible field", content: "reload_test_workers: not-an-integer", stage: "decode config"},
		{name: "invalid placeholder YAML", content: "reload_test_workers: ${PROFILE_RELOAD_BAD_VALUE:3}", env: "[", stage: "parse config"},
		{name: "invalid placeholder type", content: "reload_test_workers: ${PROFILE_RELOAD_BAD_VALUE:3}", env: "not-an-integer", stage: "decode config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := writeTestConfig(t, valid)
			previous, err := LoadEcoConfig[reloadTestConfig](configPath)
			if err != nil {
				t.Fatal(err)
			}
			want := &reloadTestConfig{Workers: 3, Prompt: "ready", Labels: map[string]string{"owner": "original"}, Items: []string{"first", "second"}}
			if !reflect.DeepEqual(previous, want) {
				t.Fatalf("initial config = %#v, want %#v", previous, want)
			}
			if tt.missing {
				if err := os.Remove(configPath); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFile(t, configPath, tt.content)
			}
			t.Setenv("PROFILE_RELOAD_BAD_VALUE", tt.env)
			failed, err := LoadEcoConfig[reloadTestConfig](configPath)
			if err == nil || failed != nil {
				t.Fatalf("failed load = %#v, %v; want nil config and error", failed, err)
			}
			if !strings.Contains(err.Error(), tt.stage) || !strings.Contains(err.Error(), configPath) {
				t.Fatalf("error lacks stage or config path: %v", err)
			}
			if (tt.missing || tt.name == "missing include") && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("error does not wrap os.ErrNotExist: %v", err)
			}
			if !reflect.DeepEqual(previous, want) {
				t.Fatalf("failed load modified previous config: %#v", previous)
			}

			writeFile(t, configPath, strings.ReplaceAll(valid, "original", "updated"))
			repaired, err := LoadEcoConfig[reloadTestConfig](configPath)
			if err != nil {
				t.Fatalf("load repaired file: %v", err)
			}
			if repaired.Labels["owner"] != "updated" {
				t.Fatalf("repaired config = %#v", repaired)
			}
			repaired.Items[0] = "changed"
			if !reflect.DeepEqual(previous, want) {
				t.Fatalf("later load shares data with previous config: %#v", previous)
			}
		})
	}
}

func TestLoadEcoConfigEnvironmentAndMapstructure(t *testing.T) {
	type config struct {
		AppConfig `mapstructure:",squash"`
		Missing   string `mapstructure:"profile_test_missing"`
		Empty     string `mapstructure:"profile_test_empty"`
		Explicit  string `mapstructure:"profile_test_explicit"`
		Override  string `mapstructure:"profile_test_override"`
		Cleared   string `mapstructure:"profile_test_cleared"`
		EnvOnly   string `mapstructure:"profile_test_env_only"`
		Nested    struct {
			Value string `mapstructure:"value"`
		} `mapstructure:"profile_test_nested"`
	}
	t.Setenv("PROFILE_TEST_PLACEHOLDER_MISSING", "")
	if err := os.Unsetenv("PROFILE_TEST_PLACEHOLDER_MISSING"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROFILE_TEST_PLACEHOLDER_EMPTY", "")
	t.Setenv("PROFILE_TEST_PLACEHOLDER_EXPLICIT", "environment")
	t.Setenv("PORT", "9090")
	t.Setenv("PROFILE_TEST_OVERRIDE", "automatic-env")
	t.Setenv("PROFILE_TEST_CLEARED", "")
	t.Setenv("PROFILE_TEST_ENV_ONLY", "not-bound")
	t.Setenv("PROFILE_TEST_NESTED.VALUE", "dotted-env")
	t.Setenv("PROFILE_TEST_NESTED_VALUE", "underscore-must-not-win")
	configPath := writeTestConfig(t, `
port: "8080"
profile_test_missing: ${PROFILE_TEST_PLACEHOLDER_MISSING:missing-default}
profile_test_empty: ${PROFILE_TEST_PLACEHOLDER_EMPTY:empty-default}
profile_test_explicit: "${PROFILE_TEST_PLACEHOLDER_EXPLICIT:default}"
profile_test_override: ${PROFILE_TEST_PLACEHOLDER_OVERRIDE:default}
profile_test_cleared: from-file
profile_test_nested:
  value: from-file
`)
	got, err := LoadEcoConfigWithValues[config](configPath, map[string]any{
		"PROFILE_TEST_PLACEHOLDER_EXPLICIT": "",
		"PROFILE_TEST_PLACEHOLDER_OVERRIDE": "explicit-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 9090 || got.Missing != "missing-default" || got.Empty != "empty-default" ||
		got.Explicit != "" || got.Override != "automatic-env" || got.Cleared != "" ||
		got.EnvOnly != "" || got.Nested.Value != "dotted-env" {
		t.Fatalf("environment/mapstructure semantics changed: %#v", got)
	}
	t.Setenv("PORT", "")
	got, err = LoadEcoConfig[config](configPath)
	if err != nil || got.Port != 0 {
		t.Fatalf("empty numeric environment = %#v, %v; want port 0", got, err)
	}
	t.Setenv("PORT", "invalid-port")
	got, err = LoadEcoConfig[config](configPath)
	if err == nil || got != nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("invalid environment type = %#v, %v; want decode error", got, err)
	}
}

func TestLoadEcoConfigIncludesRemainLiteral(t *testing.T) {
	configDir := t.TempDir()
	const literal = "key: ${PROFILE_INCLUDE_LITERAL:default}\n!include missing-nested.txt\n# keep this comment\n"
	t.Setenv("PROFILE_INCLUDE_LITERAL", "must-not-expand")
	writeTestFile(t, configDir, "shared.txt", literal)
	writeTestFile(t, configDir, "profiles/ecosystem.yaml", "reload_test_prompt: !include ../shared.txt")
	got, err := LoadEcoConfig[reloadTestConfig](filepath.Join(configDir, "profiles", "ecosystem.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != literal {
		t.Fatalf("include content changed: %q", got.Prompt)
	}
}

func TestLoadEcoConfigConcurrentIsolation(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("reload_test_workers", 9999)
	viper.SetEnvPrefix("unrelated")
	global := viper.GetViper()
	previousDefault := DefaultConfig
	const count = 16
	paths := make([]string, count)
	for i := range paths {
		paths[i] = writeTestConfig(t, fmt.Sprintf("reload_test_workers: %d\nreload_test_labels: {owner: config-%d}\nreload_test_items: [item-%d]\n", i, i, i))
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, configPath := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := 0; n < 30; n++ {
				got, err := LoadEcoConfig[reloadTestConfig](configPath)
				if err != nil {
					t.Errorf("load %s: %v", configPath, err)
					return
				}
				if got.Workers != i || got.Labels["owner"] != fmt.Sprintf("config-%d", i) ||
					!reflect.DeepEqual(got.Items, []string{fmt.Sprintf("item-%d", i)}) {
					t.Errorf("cross-file contamination for %s: %#v", configPath, got)
					return
				}
				if viper.GetInt("reload_test_workers") != 9999 {
					t.Error("global Viper modified during load")
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	if viper.GetViper() != global || viper.GetEnvPrefix() != "unrelated" || DefaultConfig != previousDefault {
		t.Fatal("loading modified global state")
	}
}

func TestReloadProfileSelectionAndErrors(t *testing.T) {
	configDir := t.TempDir()
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(configDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Error(err)
		}
	})
	writeTestFile(t, configDir, "ecosystem.yaml", "reload_test_workers: 1")
	writeTestFile(t, configDir, "ecosystem-dev.yaml", "reload_test_workers: 2")
	writeTestFile(t, configDir, "profiles/ecosystem.yaml", "reload_test_workers: 3")
	writeTestFile(t, configDir, "profiles/ecosystem-dev.yaml", "reload_test_workers: ${PROFILE_RELOAD_WORKERS:4}")
	for _, tt := range []struct {
		active string
		dir    string
		want   int
	}{
		{want: 1},
		{active: "dev", want: 2},
		{dir: "profiles", want: 3},
		{active: "dev", dir: filepath.Join(configDir, "profiles"), want: 4},
	} {
		t.Run(fmt.Sprintf("%s/%s", tt.dir, tt.active), func(t *testing.T) {
			t.Setenv("ACTIVE_PROFILE", tt.active)
			t.Setenv("ACTIVE_PROFILE_PATH", tt.dir)
			got, err := Reload[reloadTestConfig]()
			if err != nil {
				t.Fatal(err)
			}
			wantProfile := ""
			if tt.active != "" {
				wantProfile = "-" + tt.active
			}
			if got.Config.Workers != tt.want || got.ActiveProfile != wantProfile {
				t.Fatalf("Reload = %#v (%#v)", got, got.Config)
			}
		})
	}
	t.Setenv("ACTIVE_PROFILE", "dev")
	t.Setenv("ACTIVE_PROFILE_PATH", "profiles")
	got, err := ReloadWithValues[reloadTestConfig](map[string]any{"PROFILE_RELOAD_WORKERS": 5})
	if err != nil || got.Config.Workers != 5 {
		t.Fatalf("ReloadWithValues = %#v, %v", got, err)
	}
	writeTestFile(t, configDir, "profiles/ecosystem-dev.yaml", "reload_test_workers: invalid")
	failed, err := Reload[reloadTestConfig]()
	if err == nil || failed != nil || got.Config.Workers != 5 {
		t.Fatalf("Reload failure = %#v, %v; previous = %#v", failed, err, got.Config)
	}
	t.Setenv("ACTIVE_PROFILE", "missing")
	if failed, err := Reload[reloadTestConfig](); failed != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Reload missing file = %#v, %v", failed, err)
	}
}

func TestReloadDefaultOnlyReplacesOnSuccess(t *testing.T) {
	previous := DefaultConfig
	t.Cleanup(func() { DefaultConfig = previous })
	t.Setenv("ACTIVE_PROFILE", "")
	t.Setenv("PORT", "8080")
	configPath := writeTestConfig(t, "port: 8080\nmongo: {url: original}")
	t.Setenv("ACTIVE_PROFILE_PATH", filepath.Dir(configPath))
	ReloadDefault()
	if DefaultConfig == nil || DefaultConfig.Config.Port != 8080 || DefaultConfig.Config.Uri != "original" {
		t.Fatalf("ReloadDefault = %#v", DefaultConfig)
	}
	loaded := DefaultConfig
	writeFile(t, configPath, "mongo: [")
	ReloadDefault()
	if DefaultConfig != loaded || loaded.Config.Uri != "original" {
		t.Fatal("ReloadDefault replaced the previous config on failure")
	}
	writeFile(t, configPath, "mongo: {url: updated}")
	ReloadDefault()
	if DefaultConfig == loaded || DefaultConfig.Config.Uri != "updated" || loaded.Config.Uri != "original" {
		t.Fatal("ReloadDefault did not isolate successive configs")
	}
}

func TestWatchConfigKeepsGlobalViperFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	configPath := writeTestConfig(t, "watched: before")
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	otherPath := writeTestConfig(t, "reload_test_workers: 7")
	if _, err := LoadEcoConfig[reloadTestConfig](otherPath); err != nil {
		t.Fatal(err)
	}
	if viper.ConfigFileUsed() != configPath || viper.GetString("watched") != "before" {
		t.Fatal("loading replaced global Viper config or watch path")
	}
	changed := make(chan struct{}, 1)
	WatchConfig(func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if _, err := LoadEcoConfig[reloadTestConfig](otherPath); err != nil {
		t.Fatal(err)
	}
	writeFile(t, configPath, "watched: after")
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("WatchConfig did not notify for the globally configured file")
	}
}
