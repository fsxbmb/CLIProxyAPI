# CLIProxyAPI Lite

面向 macOS 个人使用的本机 AI 模型网关。它把多个订阅账号和 OpenAI-compatible 上游统一成一个本地 API，让 Codex 之外的 Agent、CLI 和编辑器插件也能使用这些模型。

这个仓库不是对上游源码的原样复制。它使用 [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 的 Go SDK 作为协议与 OAuth 引擎，在外围提供更小的本机启动器、安全默认值和一个无前端框架的内嵌 Web 管理界面。

## 当前范围

| 能力 | 接入方式 | 状态 |
| --- | --- | --- |
| 两个 ChatGPT Plus | Codex OAuth；重复登录两个不同账号 | 支持 |
| Claude | Claude Code OAuth | 支持 |
| Gemini | Antigravity OAuth | 支持 |
| Grok | xAI / Grok Build OAuth | 支持 |
| Qwen | DashScope 或其他 OpenAI-compatible API | 支持 |
| 第三方中转 | OpenAI-compatible Base URL + API key | 支持 |
| 多账号路由 | Round-robin、失败切换、凭据冷却 | 支持 |
| Web 管理 | 账号、中转、模型列表、最小请求测试、诊断 | 支持 |

刻意不包含云同步、远程管理、遥测、移动端、集群数据库、Docker 配置和上游远程下载的管理面板。动态插件默认关闭；上述厂商不依赖它。

## 运行结构

- `http://127.0.0.1:8317`：给 Agent / CLI 使用的代理 API。
- `http://127.0.0.1:8318/ui/`：只允许本机访问的管理界面。
- `~/Library/Application Support/CLIProxyAPI-Lite`：配置、OAuth 文件和本机密钥。

API 与 Web UI 是同一个进程中的两个本机监听器。Web UI 只把管理请求反向代理给本机 API，不会把密钥发送到其他服务器。

## 在 Mac 上开始使用

要求 macOS 13 或更高版本，以及 Go 1.26。没有 Go 时可先安装：

```bash
brew install go
```

然后构建并启动：

```bash
git clone git@github.com:fsxbmb/CLIProxyAPI.git
cd CLIProxyAPI
make build
./bin/cliproxy-lite init
./bin/cliproxy-lite serve
```

服务就绪后会自动打开 Web UI。首次连接时，在另一个终端查看管理密钥：

```bash
./bin/cliproxy-lite keys
```

也可以安装到 `~/bin`：

```bash
./install.sh
cliproxy-lite serve
```

如果 shell 找不到命令，把 `~/bin` 加入 `PATH`。

## 连接两个 ChatGPT Plus

1. 启动服务并在 Web UI 输入 Management key。
2. 打开“OAuth 账号”，点击“ChatGPT Plus → 登录账号”。
3. 在浏览器完成第一个账号授权。
4. 再点一次登录，并在 OpenAI 登录页选择另一个 ChatGPT Plus 账号。
5. 页面中的 `GPT Plus` 指标应显示 `2 / 2`。

OAuth 凭据只保存在本机 auth 目录。停用不会删除文件；删除只移除本机凭据，不会删除厂商账号。

## 添加 Qwen 或第三方中转

打开“中转与 Qwen”。Qwen 可以点击“填入 Qwen 预设”，补充 DashScope API key 并核对模型名。其他中转填写：

- 显示名称；
- 可选模型前缀，用于避免同名模型冲突；
- 以 `/v1` 结尾的 OpenAI-compatible Base URL；
- 上游 API key；
- 每行一个 `上游模型=客户端模型名` 的模型映射。

保存后配置会热重载，无需重启。

## 让 Agent / CLI 接入

先运行 `cliproxy-lite keys` 获取本机 API key。

通用 OpenAI-compatible 客户端：

```bash
export OPENAI_BASE_URL="http://127.0.0.1:8317/v1"
export OPENAI_API_KEY="sk-local_..."
```

Codex 自定义 provider 示例：

```toml
[model_providers.local_proxy]
name = "CLIProxyAPI Local"
base_url = "http://127.0.0.1:8317/v1"
env_key = "CLIPROXY_API_KEY"
wire_api = "responses"

[profiles.local_proxy]
model_provider = "local_proxy"
model = "gpt-5.4"
```

```bash
export CLIPROXY_API_KEY="sk-local_..."
codex --profile local_proxy
```

使用 Anthropic 协议的工具通常设置：

```bash
export ANTHROPIC_BASE_URL="http://127.0.0.1:8317"
export ANTHROPIC_API_KEY="sk-local_..."
```

具体变量名取决于客户端；核心是把 Base URL 指向本机端口，并使用生成的本机 API key。

## 命令

```text
cliproxy-lite serve [--home PATH] [--ui-port 8318] [--no-open]
cliproxy-lite init [--home PATH]
cliproxy-lite keys [--home PATH]
cliproxy-lite doctor [--home PATH]
cliproxy-lite version
```

`doctor` 会检查 YAML、目录权限、本机监听和远程管理设置。也可用 `CLIPROXY_LITE_HOME` 改变数据目录，便于测试或维护多套配置。

## 安全与隐私

- 进程拒绝以 `0.0.0.0` 或局域网 IP 启动，并拒绝开启远程管理。
- 数据目录使用 `0700`，配置、密钥和 OAuth 文件使用仅当前用户可读的权限。
- Web UI 的 Management key 只存于 `sessionStorage`，关闭标签页后消失。
- 请求正文日志、文件日志、用量统计和 pprof 默认关闭。
- 上游自带的远程管理面板下载与自动更新被禁用。
- 不要提交 `config.yaml`、`secrets.json` 或 auth 目录，也不要把本机端口暴露到公网。

OAuth 转发和订阅账号的使用仍受各厂商条款约束。本项目定位为个人本机工具，不应用于共享账号、转售额度或向公网提供服务。

## 开发

```bash
go mod tidy
go test ./...
go vet ./...
make build
```

当前固定使用 CLIProxyAPI SDK `v7.2.139`，避免上游接口变化导致不可重复构建。升级依赖时需要重新验证 OAuth、管理 API 和模型转换。

## 许可证

本仓库代码使用 MIT License。上游 CLIProxyAPI 及其依赖各自遵循对应许可证，详见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
