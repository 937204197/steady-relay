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

## 安全边界与兼容性

- 本项目不会提供或默认使用任何第三方上游。你必须自行设置 `UPSTREAM_BASE_URL`。
- API Key、请求体和响应内容会被转发到该上游；只配置你有权使用且信任的服务。
- 默认仅监听 `127.0.0.1`。不要改为 `0.0.0.0`，因为代理没有本地访问鉴权。
- 请求体与响应体不会被改写；仅移除 HTTP 规定不能转发的 hop-by-hop 头。
- 当 SSE 已有文本、输出项或工具调用发给客户端后，代理不会重试，避免重复内容或
  重复工具调用。实际输出前的临时失败可以安全重试。
- 固定 IP 是可选功能；HTTPS 仍使用原域名完成 Host、SNI 和证书校验。

## 快速开始

下载 GitHub Releases 中与你系统匹配的压缩包并完整解压。发布包是独立程序，不需要
Python 或 Go。

先配置你自己的上游地址。地址通常应包含 `/v1`。

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

启动成功后，终端会打印实际端口。将客户端的 API Base URL 设置为：

```text
http://127.0.0.1:8080/v1
```

如果 8080 已被占用，程序会使用 8081、8082 等后续端口；请以启动日志为准。代理
仅接受 `/v1` 或 `/v1/*` 路径：Base URL 漏写 `/v1` 会导致 `/responses` 被拒绝为 404。

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

默认会在首次请求后最多重试 10 次，即最多 11 次上游请求。退避使用带抖动的指数
等待，后续重试位于各自等待窗口的后半段，单次最多 60 秒。所有 `429`（包括
`usage_limit_reached`）都可重试，方便上游在多账号或多渠道间切换；全部失败时，最后
一次上游 HTTP 错误会原样转发给客户端。

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
./scripts/build-release.sh 2.1.4
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
