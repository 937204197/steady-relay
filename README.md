# Steady Relay

一个本地运行的 OpenAI 兼容 API 重试代理。它接收发往本机 `/v1/*` 的请求，并把
请求透明转发到你**明确配置且信任**的上游；遇到连接错误、超时或临时性
`408/425/429/5xx` 时会退避重试。

> 非官方项目，与 OpenAI 没有关联，也未获 OpenAI 认可。

## 这个项目解决什么问题？

第三方模型平台在渠道繁忙、账号额度用尽或服务容量不足时，可能返回
`model_capacity`、`server_is_overloaded`、`server_unavailable`、
`usage_limit_reached` 等错误。有些客户端会把这类错误直接视为本次任务失败，
不会在错误发生后继续重新发起请求，导致 Codex 任务中断。

Steady Relay 放在客户端和模型平台之间，先接收客户端请求，再透明转发到你配置的
上游。当上游在尚未产生实际文本、输出项或工具调用时返回上述临时性错误，代理会在
错误交给客户端前按退避策略自动重试；默认最多重试 10 次（首次请求加起来最多 11
次）。上游可能先发送 `keepalive`/心跳 SSE 事件再报告容量错误；这类心跳不会被当作
实际输出，因此仍可触发安全重试。因此，搜索或遇到 `model_capacity`、`server_is_overloaded`、
`server_unavailable`、`usage_limit_reached`、`429`、`503` 等错误时，可以尝试在
客户端前增加 Steady Relay，并将客户端的 API Base URL 指向本地代理。

代理不会改写请求体或响应体。若上游已经向客户端发送了实际输出，代理不会重放该
请求，以避免重复文本、重复工具调用或重复执行。

### 可选增强模式：成功后再放行 SSE

如果上游经常在已经发送 `response.output_text.delta` 或工具调用片段后，才返回
`server_is_overloaded`，可以主动开启 `--buffer-until-success`（或设置
`BUFFER_UNTIL_SUCCESS=true`）。开启后，代理会先在本地缓存本次 SSE 响应，不向 Codex
发送任何内容；只有收到 `response.completed` 才一次性放行。若收到
`response.failed`、连接中断或其他未完成终态，缓存会被丢弃并按重试策略重新请求，
从而避免把失败尝试的半截文本或工具调用交给 Codex。

该模式默认关闭，因为它会增加首字节等待时间和内存占用。为避免异常响应无限占用内存，
单次尝试的缓存上限为 64 MiB；超过上限会丢弃本次尝试并重试。重试耗尽后仍未收到
`response.completed` 时，代理不会放行不完整输出；如果最终收到的是
`response.failed`/`error`，只会放行最后的错误事件，不会放行此前的半截输出；如果连接
中断或超过缓存上限，则返回上游不可用错误。该模式仅适用于 Go 独立程序；Python 备用
实现不支持此选项。

## 安全边界与兼容性

- 本项目不会提供或默认使用任何第三方上游。你必须自行设置 `UPSTREAM_BASE_URL`。
- API Key、请求体和响应内容会被转发到该上游；只配置你有权使用且信任的服务。
- 默认仅监听 `127.0.0.1`。不要改为 `0.0.0.0`，因为代理没有本地访问鉴权。
- 请求体与响应体不会被改写；仅移除 HTTP 规定不能转发的 hop-by-hop 头。
- 当 SSE 已有文本、输出项或工具调用发给客户端后，代理不会重试，避免重复内容或
  重复工具调用。实际输出前的临时失败可以安全重试。
- 固定 IP 是可选功能；HTTPS 仍使用原域名完成 Host、SNI 和证书校验。

## 快速开始（普通用户无需开发环境）

发布包是独立程序，不需要安装 Python、Go 或其他开发工具。请先从上面的 Release
下载与你电脑匹配的压缩包，并**完整解压**到一个普通文件夹；不要直接在 ZIP 压缩包
预览窗口里运行程序。

### 下载 v2.2.0

请按操作系统和处理器架构选择安装包。链接文字使用易于理解的系统名称，括号中的架构
用于确认与你的电脑匹配：

| 系统与芯片 | 下载 |
| --- | --- |
| macOS（Intel 芯片，x86_64） | [下载 macOS Intel 版](https://github.com/937204197/steady-relay/releases/download/v2.2.0/steady-relay-2.2.0-darwin-amd64.tar.gz) |
| macOS（Apple 芯片，ARM64；M1/M2/M3 等） | [下载 macOS Apple 芯片版](https://github.com/937204197/steady-relay/releases/download/v2.2.0/steady-relay-2.2.0-darwin-arm64.tar.gz) |
| Windows（Intel/AMD 64 位，x64） | [下载 Windows x64 版](https://github.com/937204197/steady-relay/releases/download/v2.2.0/steady-relay-2.2.0-windows-amd64.zip) |
| Windows（ARM64） | [下载 Windows ARM64 版](https://github.com/937204197/steady-relay/releases/download/v2.2.0/steady-relay-2.2.0-windows-arm64.zip) |
| Linux（Intel/AMD 64 位，x86_64） | [下载 Linux x86_64 版](https://github.com/937204197/steady-relay/releases/download/v2.2.0/steady-relay-2.2.0-linux-amd64.tar.gz) |
| Linux（ARM64，aarch64） | [下载 Linux ARM64 版](https://github.com/937204197/steady-relay/releases/download/v2.2.0/steady-relay-2.2.0-linux-arm64.tar.gz) |

也可以下载 [SHA256 校验文件](https://github.com/937204197/steady-relay/releases/download/v2.2.0/SHA256SUMS)，
验证安装包完整性。所有平台的完整文件列表和历史版本见
[GitHub Releases](https://github.com/937204197/steady-relay/releases)。

### 最简单的启动方式

你需要准备一个自己信任、且有权使用的 OpenAI 兼容模型 API 地址，通常以 `/v1` 结尾，
例如 `https://api.example.com/v1`。API Key 仍由 Codex 保存并发送，Steady Relay 不会
要求你把 API Key 写进脚本。

#### Windows

1. 打开解压后的文件夹，确认能看到 `start.bat` 和 `steady-relay.exe`。
2. 双击 `start.bat`。如果没有预先配置上游地址，程序会在窗口中提示你输入；粘贴地址
   后按回车即可。
   如果你下载的是较早的安装包、窗口没有输入提示：点击文件夹顶部的地址栏，输入
   `cmd` 并按回车，然后依次粘贴下面两行；这样不需要自己执行 `cd`：

   ```bat
   set UPSTREAM_BASE_URL=https://api.example.com/v1
   start.bat
   ```

3. 如果 Windows SmartScreen 弹出提示，先确认文件来自本项目的 GitHub Release，再点
   “更多信息”→“仍要运行”。
4. 保持这个黑色窗口打开，Codex 使用期间不要关闭它。按窗口提示的本地地址配置 Codex。

#### macOS

1. 打开解压后的文件夹，确认能看到 `install-macos.command` 和 `steady-relay`。
2. 按住 Control 键点击 `install-macos.command`，选择“打开”，再在确认窗口中点击“打开”。
   首次运行时这是 macOS 对未签名程序的正常提示。
3. 在终端提示处粘贴你的上游 API 地址（通常以 `/v1` 结尾）并按回车。
4. 保持打开的终端窗口，不要关闭它。

如果 macOS 没有显示“打开”选项，请先右键文件选择“打开”；仍无法启动时，打开“系统设置
→ 隐私与安全性”，在底部点击“仍要打开”，然后重新执行第 2 步。

#### Linux

1. 完整解压压缩包。
2. 在文件管理器中打开这个文件夹，右键空白处选择“在终端中打开”（不同发行版名称可能略有不同）。
3. 在打开的终端中粘贴下面一行并按回车：

   ```bash
   ./start.sh --upstream https://api.example.com/v1
   ```

如果提示没有执行权限，先运行 `chmod +x start.sh steady-relay`，再重复上一步。

## Codex 安装向导 Skill（可选）

仓库还提供了一个面向普通用户的 `steady-relay-setup` Skill。安装后，你可以把本仓库的
GitHub 地址发给 Codex，让它按步骤完成版本选择、下载、SHA-256 校验、解压、生成启动命令
和健康检查，不需要自己理解 `cd` 或手工拼下载链接。

### 安装 Skill

可以直接让 Codex 使用内置的 Skill 安装能力执行下面的请求：

```text
请安装这个仓库中的 Codex Skill：
https://github.com/937204197/steady-relay/tree/main/skills/steady-relay-setup
```

安装完成后，如果 Skill 没有立即出现在列表中，请重新打开 Codex。也可以在当前仓库中让
Codex 读取 `skills/steady-relay-setup/SKILL.md` 后再使用。

### 使用 Skill 安装和启动

```text
请使用 $steady-relay-setup，帮我从这个地址安装并启动 Steady Relay：
https://github.com/937204197/steady-relay
```

向导会先识别系统和芯片，展示要下载的 Release 文件和目标目录，得到确认后再下载并校验。
随后它会询问你信任的上游 API Base URL，检查本地 `/healthz`，并给出 CC Switch 的配置步骤。
它不会索取或记录 API Key，也不会默认修改 CC Switch 数据库或 Codex 配置文件。

使用期间必须保持 Steady Relay 的终端窗口或进程运行；不用时先停止中转，再把 Codex/CC Switch
的 API 请求地址改回原来的远程地址，否则 Codex 下一次请求仍会指向已经关闭的本地地址。

### 配置 Codex

启动成功后，终端会打印实际端口。把 Codex 的 API Base URL 设置为：

```text
http://127.0.0.1:8080/v1
```

如果 8080 已被占用，程序会自动尝试 8081、8082 等后续端口；此时必须使用启动日志显示的
实际端口，例如 `http://127.0.0.1:8081/v1`。地址末尾的 `/v1` 不能省略，否则 Codex
发送 `/responses` 时会收到 404。

#### 以 CC Switch 配置 Codex（图形界面示例）

如果你使用 CC Switch 管理 Codex 的供应商，可以按下面步骤配置：

1. 先启动 Steady Relay，并按提示输入真实的第三方上游 API 地址。记下终端日志中的本地
   API 地址，默认是 `http://127.0.0.1:8080/v1`；如果端口被占用，请使用日志显示的实际端口。
2. 打开 CC Switch，进入要给 Codex 使用的供应商编辑页面。
3. 在截图中红框所示的“API 请求地址”一栏，填入上一步的本地地址，然后点击“应用”或“保存”。
   地址末尾的 `/v1` 必须保留。
4. 完全退出并重新启动 Codex，使新的地址生效；使用 Codex 期间请保持 Steady Relay 的窗口打开。

这里的“API 请求地址”应填写 Steady Relay 的本地地址（对 CC Switch 来说，这是 Codex 要连接的
上游地址），不要再次填写远程第三方 API 地址，否则请求会绕过中转，无法使用自动重试。远程第三方
API 地址应在启动 Steady Relay 时作为 `UPSTREAM_BASE_URL` 输入。看到日志中的
`[request] ... forwarding` 即表示请求已经进入中转。API Key 仍填写在 CC Switch/Codex
自己的配置中，不要写进公开脚本。

### 命令行启动（熟悉终端的用户）

先cd到解压后的文件目录

Windows 命令提示符：

```bat
set UPSTREAM_BASE_URL=https://api.example.com/v1
start.bat
```

macOS / Linux：

```bash
UPSTREAM_BASE_URL=https://api.example.com/v1 ./start.sh
```

也可以显式传入参数：

```bash
./start.sh --upstream https://api.example.com/v1
```

健康检查：

```text
http://127.0.0.1:8080/healthz
```

## 配置

命令行参数优先于环境变量。

| 参数 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `--upstream URL` | `UPSTREAM_BASE_URL` | 无，必须设置 | 上游 OpenAI 兼容 API Base URL |
| `--upstream-ip IP` | `UPSTREAM_IP` | 空 | Go 独立程序的可选固定 IP；留空使用系统 DNS |
| `--listen ADDRESS` | `LISTEN_ADDR` | `127.0.0.1:8080` | 本地监听地址 |
| `--max-retries N` | `MAX_RETRIES` | `10` | 首次请求后的最多重试次数 |
| `--retry-backoff TIME` | `RETRY_BACKOFF` | `500ms` | 指数退避基数 |
| `--request-timeout TIME` | `REQUEST_TIMEOUT` | `120s` | 上游响应头等待上限 |
| `--max-retry-after TIME` | `MAX_RETRY_AFTER` | `60s` | 上游 `Retry-After` 等待上限 |
| `--buffer-until-success` | `BUFFER_UNTIL_SUCCESS` | `false` | 缓存 SSE，收到 `response.completed` 后才放行 |

默认会在首次请求后最多重试 10 次，即最多 11 次上游请求。退避使用带抖动的指数
等待，后续重试位于各自等待窗口的后半段，单次最多 60 秒。所有 `429`（包括
`usage_limit_reached`）都可重试，方便上游在多账号或多渠道间切换；全部失败时，最后
一次上游 HTTP 错误会原样转发给客户端。

启用增强模式示例：

```bash
./start.sh --upstream https://api.example.com/v1 --buffer-until-success
```

或在 Windows 命令提示符中：

```bat
set BUFFER_UNTIL_SUCCESS=true
start.bat
```

### 可选：固定 IP 绕过异常 DNS

如果某个网络或 VPN 把你的上游域名错误解析，可由你显式提供 IP：

```bat
start.bat --upstream https://api.example.com/v1 --upstream-ip 203.0.113.10
```

这不会把 URL、HTTP Host、TLS SNI 或证书校验改为 IP。若设置了 `HTTPS_PROXY`，连接
可能由该 HTTP 代理解析域名，固定 IP 不会生效；请遵守组织网络政策。

## 日志与故障排查

正常日志不包含 API Key、完整请求体或完整响应体；只记录路径、顶层 `model`、状态、
重试、耗时和截断后的上游错误摘要。

- `[retry] ... attempt=3/10`：本地代理正在安全重试。
- `status=429 attempts=11`：首次请求加 10 次重试都未成功。
- `stream=incomplete ... context canceled`：客户端在流已提交后断开；此时不能安全重试。
- `POST /responses rejected status=404`：客户端 Base URL 缺少 `/v1`。

## 从源码运行与测试

Go 1.22+：

```bash
go test ./...
go vet ./...
UPSTREAM_BASE_URL=https://api.example.com/v1 go run .
```

Python 标准库备用实现：

```bash
python3 -m unittest -v
UPSTREAM_BASE_URL=https://api.example.com/v1 python3 proxy.py
```

Python 备用实现不支持 `UPSTREAM_IP`；它使用系统 DNS。其 `RETRY_BACKOFF` 环境变量
以秒为单位，例如 `0.5`；Go 版使用 Go duration，例如 `500ms`。

## 构建发布包

```bash
./scripts/build-release.sh 2.2.0
```

产物会写入 `dist/`。发布时请在 GitHub Release 中上传六个平台压缩包与
`SHA256SUMS`，不要将 `dist/` 提交到源码仓库。

## 首次信任发布包

当前二进制尚未代码签名。请只从可信 GitHub Release 下载，并先验证 SHA-256。

- macOS：确认来源后，可右键打开 `install-macos.command`；它只清除当前解压目录的
  quarantine 标记，不会关闭 Gatekeeper。若尚未配置上游，它会在终端询问 API Base URL，
  仅用于本次启动且不会保存。
- Windows：确认 SHA-256 和来源后，在 SmartScreen 中选择“更多信息”→“仍要运行”。
  不要关闭 Defender、SmartScreen 或防火墙。

更多使用细节见发布包内的 `README.txt` 与 `TRUST-GUIDE.zh-CN.txt`。

## 参与与安全报告

请阅读 [CONTRIBUTING.md](CONTRIBUTING.md)。安全漏洞请不要在公开 Issue 中披露，详见
[SECURITY.md](SECURITY.md)。本项目采用 [MIT License](LICENSE)。
