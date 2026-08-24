# CLIProxyAPI Lite

CLIProxyAPI Lite 是一个**跨平台、本机运行的 AI API 网关**。它把多个 OAuth 账号和 OpenAI-compatible 上游统一为本机 API，让 Agent、CLI、编辑器插件和其他应用通过同一个 OpenAI-compatible 地址访问模型。

它不是必须安装的桌面 GUI 应用，而是一个包含内嵌 Web 管理界面的独立可执行服务：

- API：`http://127.0.0.1:8317/v1`
- Web UI：`http://127.0.0.1:8318/ui/`
- 默认只监听回环地址，不接受局域网或公网访问

## 支持的平台

- macOS：arm64、amd64
- Linux：amd64、arm64
- Windows：amd64、arm64

核心服务使用 Go 构建，Web UI 使用 `go:embed` 嵌入二进制，不需要 Node.js。发布包中包含对应平台的可执行文件；运行目标机器不需要安装 Go。

## 发布包是什么

Release 下载包是按平台生成的压缩包，不是传统安装向导：

```text
cliproxy-lite_vX.Y.Z_darwin_arm64.tar.gz
cliproxy-lite_vX.Y.Z_darwin_amd64.tar.gz
cliproxy-lite_vX.Y.Z_linux_amd64.tar.gz
cliproxy-lite_vX.Y.Z_linux_arm64.tar.gz
cliproxy-lite_vX.Y.Z_windows_amd64.zip
cliproxy-lite_vX.Y.Z_windows_arm64.zip
SHA256SUMS
```

压缩包包含独立可执行文件、示例配置、许可证和对应的安装/自启动脚本。它目前不是 `.app`、`.msi`、`.deb` 或 `.rpm`；如需要，这些格式可以在此基础上另行制作。

## 快速开始

### macOS / Linux

解压后：

```bash
chmod +x cliproxy-lite
./cliproxy-lite init
./cliproxy-lite keys
./cliproxy-lite serve
```

无桌面或不想自动打开浏览器时：

```bash
./cliproxy-lite serve --no-open
```

然后打开 `http://127.0.0.1:8318/ui/`，输入 `keys` 命令显示的 Management key。

也可以从源码构建：

```bash
./install.sh
~/.local/bin/cliproxy-lite init
~/.local/bin/cliproxy-lite serve
```

### Windows PowerShell

解压后：

```powershell
.\cliproxy-lite.exe init
.\cliproxy-lite.exe keys
.\cliproxy-lite.exe serve
```

也可以使用安装脚本：

```powershell
.\scripts\install.ps1
& "$env:LOCALAPPDATA\CLIProxyAPI-Lite\cliproxy-lite.exe" init
& "$env:LOCALAPPDATA\CLIProxyAPI-Lite\cliproxy-lite.exe" serve
```

源码树和 Release 压缩包都使用同一个 `scripts\install.ps1`。如果源码树中没有预编译的 `bin\cliproxy-lite.exe`，脚本会调用 Go 1.26 或更高版本进行构建。

## 默认数据目录

程序使用 Go 的 `os.UserConfigDir()` 选择平台目录：

| 平台 | 默认目录 |
| --- | --- |
| macOS | `~/Library/Application Support/CLIProxyAPI-Lite` |
| Linux | `~/.config/CLIProxyAPI-Lite` |
| Windows | `%AppData%\CLIProxyAPI-Lite` |

也可以通过环境变量或命令参数指定：

```bash
export CLIPROXY_LITE_HOME=/path/to/cliproxy-data
cliproxy-lite serve --home /path/to/cliproxy-data
```

数据目录包含：

```text
config.yaml       # 本机服务配置
secrets.json      # Management key 和本机 API key
auth/             # OAuth 凭据
plugins/          # 可选插件目录
```

不要把真实数据目录、OAuth 文件或 `secrets.json` 提交到 Git 或上传到公共网盘。

## 网络代理

所有平台都优先读取：

```text
HTTP_PROXY
HTTPS_PROXY
ALL_PROXY
NO_PROXY
```

还可以在 `config.yaml` 中设置上游代理：

```yaml
proxy-url: "http://127.0.0.1:7890"
```

显式配置优先于自动探测。macOS 额外支持系统代理和 Clash Verge 配置探测；Windows/Linux 建议使用环境变量或 `proxy-url`。

本机 API 和 Web UI 默认加入 `NO_PROXY`：`127.0.0.1,localhost,::1`。

## 配置账号和中转

打开 Web UI 后可以：

- 登录 ChatGPT/Codex、Gemini/Antigravity、Grok 等 OAuth 账号；
- 配置 DeepSeek、Qwen、OpenRouter 或其他 OpenAI-compatible API；
- 查看可用模型；
- 测试最小请求；
- 管理账号启停；
- 查看官方账号额度；
- 对已知第三方 API 显示余额。

第三方中转需要填写 Base URL、API key 和模型映射。余额接口因供应商不同而不同，已知供应商会自动匹配，其他供应商可以在 Web UI 中填写余额查询 URL 和 JSON 路径。

### 多账号路由策略

默认使用 `fill-first`：优先持续使用一个可用账号，直到上游报告额度耗尽或账号暂时不可用，再切换到下一个账号。这样比 `round-robin` 更有利于保持同一账号的 Prompt Cache。可在 `config.yaml` 中改为：

```yaml
routing:
  strategy: "fill-first"       # 先用完一个，再切换
  session-affinity: false
```

可选策略还包括 `round-robin`（轮询）和 `weighted-round-robin`（按账号权重轮询）。

## 让其他应用接入

通用 OpenAI-compatible 客户端使用：

```bash
export OPENAI_BASE_URL="http://127.0.0.1:8317/v1"
export OPENAI_API_KEY="sk-local_..."
```

Python：

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:8317/v1",
    api_key="sk-local_...",
)

response = client.chat.completions.create(
    model="gpt-5.6-luna",
    messages=[{"role": "user", "content": "你好"}],
)
print(response.choices[0].message.content)
```

## 自启动

### macOS

```bash
./scripts/install-macos-launchd.sh
```

### Linux systemd 用户服务

```bash
./scripts/install-systemd.sh
```

### Windows 登录启动任务

PowerShell：

```powershell
.\packaging\windows\install-task.ps1
```

这些脚本都只配置本机启动，不会开放公网端口。

## 命令

```text
cliproxy-lite serve [--home PATH] [--ui-port 8318] [--no-open]
cliproxy-lite init [--home PATH]
cliproxy-lite keys [--home PATH]
cliproxy-lite doctor [--home PATH]
cliproxy-lite version
```

## 安全说明

- API 和 Web UI 默认只监听 `127.0.0.1`；
- 远程管理默认禁用，程序拒绝不安全的监听地址；
- OAuth 文件、Management key、本机 API key 都是敏感凭据；
- 请求正文日志、遥测、用量统计和 pprof 默认关闭；
- 不要将 8317/8318 暴露到公网；
- 远程 Linux 机器建议使用 SSH 隧道，而不是修改 host 为 `0.0.0.0`。

Linux SSH 隧道示例：

```bash
ssh -L 8317:127.0.0.1:8317 -L 8318:127.0.0.1:8318 user@server
```

然后在本机浏览器打开：

```text
http://127.0.0.1:8318/ui/
```

## 开发和构建

要求 Go 1.26 或更高版本：

```bash
go mod download
go test ./...
go vet ./...
make build
```

交叉编译示例：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/cliproxy-lite-linux-amd64 ./cmd/cliproxy-lite
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o dist/cliproxy-lite-windows-amd64.exe ./cmd/cliproxy-lite
```

推送 `v*` 标签后，GitHub Actions 会构建六个平台架构、生成压缩包并发布 `SHA256SUMS`。

## 许可证

本项目代码使用 MIT License。上游 CLIProxyAPI 及其依赖遵循各自许可证，详见 `THIRD_PARTY_NOTICES.md`。
