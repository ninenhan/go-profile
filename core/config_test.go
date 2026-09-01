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

type emptyDefaultScalarConfig struct {
	PlainEmpty    string `mapstructure:"plain_empty"`
	QuotedEmpty   string `mapstructure:"quoted_empty"`
	FromEnv       string `mapstructure:"from_env"`
	FromValues    string `mapstructure:"from_values"`
	ExternalEmpty string `mapstructure:"external_empty"`
	Enabled       bool   `mapstructure:"enabled"`
	Workers       int    `mapstructure:"workers"`
	URL           string `mapstructure:"url"`
	LegacyEmpty   string `mapstructure:"legacy_empty"`
	Embedded      string `mapstructure:"embedded"`
	Repeated      string `mapstructure:"repeated"`
	SingleQuoted  string `mapstructure:"single_quoted"`
	SpecialChars  string `mapstructure:"special_chars"`
}

type emptyDefaultCollectionConfig struct {
	Nested struct {
		Value string `mapstructure:"value"`
	} `mapstructure:"nested"`
	List       []string          `mapstructure:"list"`
	InlineList []string          `mapstructure:"inline_list"`
	InlineMap  map[string]string `mapstructure:"inline_map"`
	DynamicMap map[string]string `mapstructure:"dynamic_map"`
}

type emptyDefaultBlockConfig struct {
	AnchorValue string `mapstructure:"anchor_value"`
	AliasValue  string `mapstructure:"alias_value"`
	Literal     string `mapstructure:"literal"`
	Folded      string `mapstructure:"folded"`
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

func TestLoadEcoConfigSupportsEmptyDefaultsInScalarYAML(t *testing.T) {
	t.Setenv("PROFILE_EMPTY_FROM_ENV", "from-environment")
	t.Setenv("PROFILE_EMPTY_PLAIN", "")
	config, err := LoadEcoConfigWithValues[emptyDefaultScalarConfig](
		filepath.Join("testdata", "empty-default-scalars.yaml"),
		map[string]any{
			"PROFILE_EMPTY_FROM_VALUES":    "from-values",
			"PROFILE_EMPTY_EXTERNAL_EMPTY": "",
			"PROFILE_EMPTY_BOOL":           true,
			"PROFILE_EMPTY_WORKERS":        float64(4),
			"PROFILE_EMPTY_EMBEDDED":       "middle",
			"PROFILE_EMPTY_REPEAT":         "same",
			"PROFILE_EMPTY_SINGLE_QUOTED":  "single",
			"PROFILE_EMPTY_SPECIAL_CHARS":  "value:with#characters",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := &emptyDefaultScalarConfig{
		FromEnv:      "from-environment",
		FromValues:   "from-values",
		Enabled:      true,
		Workers:      4,
		URL:          "https://example.test/api:v1",
		Embedded:     "prefix-middle-suffix",
		Repeated:     "same/same",
		SingleQuoted: "single",
		SpecialChars: "value:with#characters",
	}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("scalar YAML mismatch:\n got: %#v\nwant: %#v", config, want)
	}
}

func TestLoadEcoConfigSupportsEmptyDefaultsInCollectionYAML(t *testing.T) {
	config, err := LoadEcoConfigWithValues[emptyDefaultCollectionConfig](
		filepath.Join("testdata", "empty-default-collections.yaml"),
		map[string]any{
			"PROFILE_COLLECTION_NESTED":        "nested-value",
			"PROFILE_COLLECTION_LIST_FIRST":    "first",
			"PROFILE_COLLECTION_INLINE_FIRST":  "inline-first",
			"PROFILE_COLLECTION_MAP_SET":       "map-value",
			"PROFILE_COLLECTION_DYNAMIC_KEY":   "exact-key",
			"PROFILE_COLLECTION_DYNAMIC_VALUE": "exact-value",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if config.Nested.Value != "nested-value" {
		t.Fatalf("nested value = %q", config.Nested.Value)
	}
	if !reflect.DeepEqual(config.List, []string{"first", "second-default", ""}) {
		t.Fatalf("list = %#v", config.List)
	}
	if !reflect.DeepEqual(config.InlineList, []string{"inline-first", "inline-default", ""}) {
		t.Fatalf("inline list = %#v", config.InlineList)
	}
	if !reflect.DeepEqual(config.InlineMap, map[string]string{"empty": "", "set": "map-value"}) {
		t.Fatalf("inline map = %#v", config.InlineMap)
	}
	if !reflect.DeepEqual(config.DynamicMap, map[string]string{"exact-key": "exact-value"}) {
		t.Fatalf("dynamic map = %#v", config.DynamicMap)
	}
}

func TestLoadEcoConfigSupportsEmptyDefaultsInAnchorsAndBlocks(t *testing.T) {
	config, err := LoadEcoConfigWithValues[emptyDefaultBlockConfig](
		filepath.Join("testdata", "empty-default-blocks.yaml"),
		map[string]any{"PROFILE_BLOCK_ANCHOR": "shared"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if config.AnchorValue != "shared" || config.AliasValue != "shared" {
		t.Fatalf("anchor/alias mismatch: %#v", config)
	}
	if config.Literal != "first=\nsecond=two\n" {
		t.Fatalf("literal block = %q", config.Literal)
	}
	if config.Folded != "prefix  suffix\n" {
		t.Fatalf("folded block = %q", config.Folded)
	}
}

func TestReplaceWithValuesPlaceholderForms(t *testing.T) {
	t.Setenv("PROFILE_FORM_ENV", "environment")
	t.Setenv("PROFILE_FORM_EMPTY_ENV", "")
	tests := []struct {
		name   string
		input  string
		values map[string]any
		want   string
	}{
		{name: "no placeholder", input: "plain", want: "plain"},
		{name: "empty default missing", input: "${PROFILE_FORM_MISSING:}", want: ""},
		{name: "empty default environment", input: "${PROFILE_FORM_ENV:}", want: "environment"},
		{name: "empty environment uses default", input: "${PROFILE_FORM_EMPTY_ENV:fallback}", want: "fallback"},
		{name: "external value wins", input: "${PROFILE_FORM_ENV:fallback}", values: map[string]any{"PROFILE_FORM_ENV": "external"}, want: "external"},
		{name: "external empty is explicit", input: "${PROFILE_FORM_ENV:fallback}", values: map[string]any{"PROFILE_FORM_ENV": ""}, want: ""},
		{name: "URL default keeps colon", input: "${PROFILE_FORM_URL:https://example.test:8443/path}", want: "https://example.test:8443/path"},
		{name: "embedded empty", input: "before-${PROFILE_FORM_MISSING:}-after", want: "before--after"},
		{name: "adjacent", input: "${PROFILE_FORM_A:}${PROFILE_FORM_B:b}", want: "b"},
		{name: "repeated", input: "${PROFILE_FORM_REPEAT:x}/${PROFILE_FORM_REPEAT:x}", want: "x/x"},
		{name: "legacy quoted empty", input: `${PROFILE_FORM_MISSING:""}`, want: `""`},
		{name: "missing colon remains literal", input: "${PROFILE_FORM_MISSING}", want: "${PROFILE_FORM_MISSING}"},
		{name: "missing name remains literal", input: "${:fallback}", want: "${:fallback}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := replaceWithValues(tt.input, tt.values)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("replaceWithValues(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestScalarStringSupportsAllScalarTypes(t *testing.T) {
	tests := []struct {
		value any
		want  string
	}{
		{value: "text", want: "text"},
		{value: true, want: "true"},
		{value: float32(1.5), want: "1.5"},
		{value: float64(2.5), want: "2.5"},
		{value: int(3), want: "3"},
		{value: int8(4), want: "4"},
		{value: int16(5), want: "5"},
		{value: int32(6), want: "6"},
		{value: int64(7), want: "7"},
		{value: uint(8), want: "8"},
		{value: uint8(9), want: "9"},
		{value: uint16(10), want: "10"},
		{value: uint32(11), want: "11"},
		{value: uint64(12), want: "12"},
	}
	for _, tt := range tests {
		got, err := scalarString(tt.value)
		if err != nil {
			t.Fatalf("scalarString(%T): %v", tt.value, err)
		}
		if got != tt.want {
			t.Fatalf("scalarString(%T) = %q, want %q", tt.value, got, tt.want)
		}
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
