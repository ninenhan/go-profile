package core

import (
	"bytes"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/ninenhan/go-profile/utils"
	"github.com/spf13/viper"
	"log"
	"log/slog"
	"os"
	"path"
	"regexp"
	"strings"
)

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

func ReloadDefault() {
	if config, err := Reload[EcoSystemConfig](); err == nil {
		DefaultConfig = config
	}
}

func Reload[T any]() (*ProfileConfig[T], error) {
	active := os.Getenv("ACTIVE_PROFILE")
	activePath := os.Getenv("ACTIVE_PROFILE_PATH")
	var envValue = ""
	if active != "" {
		envValue = "-" + active
	}
	configPath := fmt.Sprintf("ecosystem%s.yaml", envValue)
	finPath := utils.Ternary(activePath != "", path.Join(activePath, configPath), configPath)
	log.Printf("Loading config envValue = %s , path = %s", active, finPath)
	config, err := LoadEcoConfig[T](finPath)
	if err != nil {
		log.Fatalln("Failed to load config", "error", err)
		return nil, err
	}
	return &ProfileConfig[T]{
		Config:        config,
		ActiveProfile: envValue,
	}, nil
}

// 替换字符串中的占位符 ${ENV_VAR:default}，
// 如果环境变量存在，则替换成环境变量的值，否则使用默认值。
func replaceWithEnvVars(input string) string {
	// 定义正则表达式，匹配 ${ENV_VAR:default_value} 格式
	re := regexp.MustCompile(`\${([^:}]+):([^}]+)}`)
	// 查找所有匹配的占位符
	matches := re.FindAllStringSubmatch(input, -1)
	// 如果没有匹配到任何占位符，直接返回原字符串
	if len(matches) == 0 {
		return input
	}
	// 遍历所有匹配的占位符并进行替换
	for _, match := range matches {
		envVar := match[1]       // 环境变量名
		defaultValue := match[2] // 默认值（可能是更复杂的字符串）
		// 获取环境变量的值，如果没有设置则使用默认值
		envValue := os.Getenv(envVar)
		if envValue == "" {
			envValue = defaultValue
		}
		// 替换原始字符串中的占位符为相应的环境变量值或默认值
		input = strings.Replace(input, match[0], envValue, -1)
	}
	// 返回最终替换后的字符串
	return input
}

func LoadEcoConfig[T any](configPath string) (*T, error) {
	// 读取整个 YAML 配置文件的字节内容
	fs, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("Error reading the config file as a string: %s", err)
	}
	str := string(fs)
	str = replaceWithEnvVars(str)
	//viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")
	viper.SetTypeByDefaultValue(true)
	//if err := viper.ReadInConfig(); err != nil {
	//	return nil, err
	//}
	// 自动绑定环境变量
	viper.AutomaticEnv()
	viper.AllowEmptyEnv(true)
	var config T
	if err := viper.ReadConfig(bytes.NewBuffer([]byte(str))); err != nil {
		log.Fatalf("Error reading config from byte data: %s", err)
	}
	// 功能欠缺
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

// WatchConfig 动态监控配置变化
func WatchConfig(callback func()) {
	viper.OnConfigChange(func(e fsnotify.Event) {
		slog.Info(fmt.Sprintf("Config file changed: %s", e.Name))
		callback()
	})
	viper.WatchConfig()
}
