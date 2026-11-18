# go-profile

通过 `github.com/ninenhan/go-profile/core` 加载 YAML 配置。

## 运行时重新加载

继续使用已有的 `LoadEcoConfig[T](configPath) (*T, error)`，无需迁移到新 API。
每次调用创建独立的 Viper 和配置对象。读取、占位符替换、YAML 解析、
`!include` 和解码失败均返回 `nil, error`，错误包含配置路径、处理阶段和原始原因。
不会退出进程，也不会改动全局 Viper、`DefaultConfig` 或此前返回的配置对象。
文件修复后可直接用同一路径再次加载。

gateway 在已有的重载处理函数中可以直接使用以下片段。
`GatewayConfig`、`configPath` 和 `validateConfig` 使用 gateway 已有的定义；
此片段所在函数返回 `error`：

```go
// import "github.com/ninenhan/go-profile/core"

candidate, err := core.LoadEcoConfig[GatewayConfig](configPath)
if err != nil {
    return err
}
if err := validateConfig(candidate); err != nil {
    return err
}
// 只有到这里才将 candidate 交给 gateway 已有的配置生效流程。
// 之前任一步失败都直接返回，现有服务继续使用原配置。
```

库仅负责读取和解码；业务校验、并发切换与配置生效由调用方负责。

## 配置语义

- `${NAME:default}` 在 YAML 解析前进行文本替换；环境变量未设置或值为空时使用
  `default`，默认值可以为空或包含冒号。`${NAME}` 等不匹配的形式保持原样。
  替换内容不额外转义，若形成非法 YAML 则返回解析错误。
- `LoadEcoConfigWithValues[T](path, values)` 仍按精确名称匹配占位符。
  显式 values 优先于占位符对应的环境变量和默认值；显式空字符串也生效。
  不支持的非标量值返回错误。
- `!include` 路径仍按 `filepath.Join(filepath.Dir(configPath), node.Value)` 解析，
  以主配置所在目录为基准。文件内容作为完整字符串插入，保留换行，不解析其中的
  YAML、不递归 include，也不替换被包含文件中的环境变量占位符。
  主文件中 include 路径上的占位符会先被替换。
- 解码保留 Viper 1.19 的 `mapstructure` 标签、`,squash`、弱类型转换和默认解码钩子。
  例如可将字符串 `"8080"` 解码为整数；无法转换的类型返回错误。
  未知字段和空配置仍遵循原有 Viper 行为，必填字段检查属于调用方的业务校验。
- Viper 的 `AutomaticEnv` 和 `AllowEmptyEnv(true)` 保持开启。对配置中已有的键，
  自动环境变量覆盖在占位符替换之后生效，因此也能覆盖显式 values 替换的结果。
  环境变量名是大写配置键，嵌套键保留点号（如 `MONGO.URL`），没有新增下划线映射。
  单独存在于环境变量、而未出现在配置键集合中的字段不会自动绑定到结构体。
  此阶段空值会覆盖文件值：字符串成为空字符串，整数按原有弱类型转换成为 `0`。
  这与占位符阶段“空值使用默认值”的行为不同。

## 旧 API 与兼容性边界

所有公共 API 签名和调用方式保持不变，解析逻辑集中在 `LoadEcoConfigWithValues`。

| 入口 | 行为 |
| --- | --- |
| `LoadEcoConfig` / `LoadEcoConfigWithValues` | 显式路径加载；无全局配置赋值、无加载日志；失败返回错误。 |
| `Reload` / `ReloadWithValues` | 保留加载日志、profile 文件选择和 `ProfileConfig[T]` 返回值；失败返回错误。 |
| `ReloadDefault` | 成功时赋值 `DefaultConfig`；失败记录日志并保留旧值。原签名不返回错误，需要处理错误时改用上述返回 error 的入口。 |
| `WatchConfig` | 继续监控全局 Viper 配置的文件；加载器不设置、更换或重置该监控。 |

`Reload` 的文件选择规则保持为：

- `ACTIVE_PROFILE` 为空时读取 `ecosystem.yaml`；非空时读取 `ecosystem-<profile>.yaml`。
- `ACTIVE_PROFILE_PATH` 非空时使用 `path.Join` 拼接上述文件名，否则相对于当前工作目录。
- 返回的 `ActiveProfile` 仍为空字符串或带前导连字符的 `-<profile>`。

两项旧副作用与可处理错误、独立加载的要求存在冲突，现有行为有意调整：

1. 原来的部分失败路径会调用 `log.Fatal` 退出进程，现在返回错误。
   依赖启动失败即退出的程序，应在程序入口自行决定退出策略。
2. 加载器不再填充或重置全局 Viper。依赖加载后再调用 `viper.Get` 读取配置的代码，
   应改为使用返回的 `*T`。

`WatchConfig` 保留原全局 Viper 包装行为。需要它的调用方应自行通过
`viper.SetConfigFile(path)` 或 Viper 搜索路径明确配置监控目标；原加载器只调用
`ReadConfig`，并未登记传入的文件路径，不能依赖 `LoadEcoConfig(path)` 自动建立该路径的监控。
已配置的全局监控不会被独立加载改动。监控回调不会自动替换此前返回的 `*T`。
该旧接口不返回错误，且依赖的 Viper 在创建文件监控器失败时仍可能退出进程；
它不在 `LoadEcoConfig` / `Reload` 的加载调用链中。本次不扩展监控 API。

`ReloadDefault` 的全局赋值也保留原有并发边界；需要并发加载时使用返回独立对象的入口，
由调用方管理生效时的同步。

## 验证

```sh
go test ./...
go test -race ./...
```

测试使用真实临时配置文件，覆盖失败与修复重载、并发隔离、全局状态隔离、环境变量、
include 原文、mapstructure/squash、profile 选择，以及全局 Viper 文件监控兼容性。
