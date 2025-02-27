package main

import (
	"github.com/ninenhan/go-profile/core"
	"log"
	"os"
)

type OverrideEcoSystemConfig struct {
	core.AppConfig   `mapstructure:",squash"`
	core.MongoConfig `mapstructure:"mongo"`
	AppId            string `mapstructure:"app_id"`
}

func main() {
	//默认配置
	_ = os.Setenv("ACTIVE_PROFILE_PATH", "./tests")
	core.ReloadDefault()
	log.Println(core.DefaultConfig.Config.MongoConfig)
	log.Printf("-----------------")
	//自定义
	_ = os.Setenv("ACTIVE_PROFILE", "dev")
	config, err := core.Reload[OverrideEcoSystemConfig]()
	if err != nil {
		panic(err)
	}
	log.Println(config.Config.AppId)

}
