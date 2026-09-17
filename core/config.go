package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/ninenhan/go-profile/utils"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var envPlaceholderPattern = regexp.MustCompile(`\${([^:}]+):([^}]*)}`)

type AppConfig struct {
	Port int `mapstructure:"port"` // 端口
}

type Route struct {
	Method       string   `mapstructure:"method"`
	Path         string   `mapstructure:"path"`
	Backend      string   `mapstructure:"backend"`
	AuthRequired bool     `mapstructure:"auth_required"`
	PublicPath   []string `mapstructure:"public_path"`
}

type MongoConfig struct {
	Uri      string `mapstructure:"url"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	DbName   string `mapstructure:"db_name"`
}

type EcoSystemConfig struct {
	AppConfig   `mapstructure:",squash"`
	MongoConfig `mapstructure:"mongo"`
}

type ProfileConfig[T any] struct {
	Config        *T     `json:"config,squash"`
	ActiveProfile string `json:"active_profile"`
}

var DefaultConfig *ProfileConfig[EcoSystemConfig]

// ReloadDefault updates DefaultConfig on success and logs failures without replacing it.
func ReloadDefault() {
	config, err := Reload[EcoSystemConfig]()
	if err != nil {
		log.Printf("Failed to load default config: %v", err)
		return
	}
	DefaultConfig = config
}

// Reload loads ecosystem[-ACTIVE_PROFILE].yaml from ACTIVE_PROFILE_PATH.
// Errors are returned to the caller without terminating the process.
func Reload[T any]() (*ProfileConfig[T], error) {
	return ReloadWithValues[T](nil)
}

// ReloadWithValues loads the active profile using values as the first source
// for existing ${NAME:default} placeholders. Keys are matched exactly.
func ReloadWithValues[T any](values map[string]any) (*ProfileConfig[T], error) {
	active := os.Getenv("ACTIVE_PROFILE")
	activePath := os.Getenv("ACTIVE_PROFILE_PATH")
	var envValue = ""
	if active != "" {
		envValue = "-" + active
	}
	configPath := fmt.Sprintf("ecosystem%s.yaml", envValue)
	finPath := utils.Ternary(activePath != "", path.Join(activePath, configPath), configPath)
	log.Printf("Loading config envValue = %s , path = %s", active, finPath)
	config, err := LoadEcoConfigWithValues[T](finPath, values)
	if err != nil {
		return nil, err
	}
	return &ProfileConfig[T]{
		Config:        config,
		ActiveProfile: envValue,
	}, nil
}

// replaceWithValues replaces ${NAME:default} placeholders. Explicit values
// take precedence over process environment variables and YAML defaults.
func replaceWithValues(input string, values map[string]any) (string, error) {
	matches := envPlaceholderPattern.FindAllStringSubmatch(input, -1)
	for _, match := range matches {
		name := match[1]
		value, exists := values[name]
		var replacement string
		if exists {
			var err error
			replacement, err = scalarString(value)
			if err != nil {
				return "", fmt.Errorf("config value %q: %w", name, err)
			}
		} else if replacement = os.Getenv(name); replacement == "" {
			replacement = match[2]
		}
		input = strings.ReplaceAll(input, match[0], replacement)
	}
	return input, nil
}

func scalarString(value any) (string, error) {
	switch value := value.(type) {
	case string:
		return value, nil
	case bool:
		return strconv.FormatBool(value), nil
	case json.Number:
		return value.String(), nil
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32), nil
	case int:
		return strconv.Itoa(value), nil
	case int8:
		return strconv.FormatInt(int64(value), 10), nil
	case int16:
		return strconv.FormatInt(int64(value), 10), nil
	case int32:
		return strconv.FormatInt(int64(value), 10), nil
	case int64:
		return strconv.FormatInt(value, 10), nil
	case uint:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint64:
		return strconv.FormatUint(value, 10), nil
	default:
		return "", fmt.Errorf("unsupported scalar type %T", value)
	}
}

// 递归把所有 tag == "!include" 的节点替换为文件内容
func resolveIncludes(node *yaml.Node, baseDir string) error {
	if node.Kind == yaml.ScalarNode && node.Tag == "!include" {
		// node.Value 是 include 后面的文件路径
		includePath := filepath.Join(baseDir, node.Value)
		data, err := os.ReadFile(includePath)
		if err != nil {
			return fmt.Errorf("!include 读取文件 %s 失败: %w", includePath, err)
		}
		// 把这个节点改成普通字符串类型的 literal block
		node.Tag = "!!str"
		node.Value = string(data)
		node.Style = yaml.LiteralStyle
		return nil
	}
	// 否则继续遍历子节点
	for _, child := range node.Content {
		if err := resolveIncludes(child, baseDir); err != nil {
			return err
		}
	}
	return nil
}

// LoadEcoConfig loads a YAML file into a new config without changing global state.
// Read, placeholder, include, parse and decode failures return a nil config and an error.
// The caller is responsible for business validation and applying the result.
func LoadEcoConfig[T any](configPath string) (*T, error) {
	return LoadEcoConfigWithValues[T](configPath, nil)
}

// LoadEcoConfigWithValues loads one config file with exact-name placeholder values.
// Like LoadEcoConfig, each call uses an independent Viper instance.
func LoadEcoConfigWithValues[T any](configPath string, values map[string]any) (*T, error) {
	fs, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", configPath, err)
	}
	str := string(fs)
	raw, err := replaceWithValues(str, values)
	if err != nil {
		return nil, fmt.Errorf("replace placeholders in config %q: %w", configPath, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &root); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", configPath, err)
	}

	baseDir := filepath.Dir(configPath)
	if err := resolveIncludes(&root, baseDir); err != nil {
		return nil, fmt.Errorf("resolve includes in config %q: %w", configPath, err)
	}

	merged, err := yaml.Marshal(&root)
	if err != nil {
		return nil, fmt.Errorf("marshal config %q: %w", configPath, err)
	}
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetTypeByDefaultValue(true)
	v.AutomaticEnv()
	v.AllowEmptyEnv(true)
	var config T
	if err := v.ReadConfig(bytes.NewReader(merged)); err != nil {
		return nil, fmt.Errorf("read YAML config %q: %w", configPath, err)
	}
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("decode config %q: %w", configPath, err)
	}
	return &config, nil
}

// WatchConfig watches the file configured on the global Viper instance.
// LoadEcoConfig and Reload do not configure this watcher or update its state.
func WatchConfig(callback func()) {
	viper.OnConfigChange(func(e fsnotify.Event) {
		slog.Info(fmt.Sprintf("Config file changed: %s", e.Name))
		callback()
	})
	viper.WatchConfig()
}
