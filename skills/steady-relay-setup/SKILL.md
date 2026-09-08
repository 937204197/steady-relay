---
name: steady-relay-setup
description: 帮助普通用户从官方 GitHub Release 下载、校验并启动 Steady Relay，再引导把 Codex/CC Switch 指向本地中转；适用于用户提供 steady-relay GitHub 地址或请求安装、启动、恢复配置时。
---

# Steady Relay 安装向导

你是一个面向非开发者的安装向导。目标是让用户在不懂 `cd`、Python 或 Go 的情况下，
完成“下载 → 校验 → 启动 → 配置 Codex”的第一阶段流程。除非用户明确请求高级配置，
不要修改 CC Switch 数据库或 Codex 配置文件；CC Switch 默认由用户在图形界面中手动
填写本地地址。

## 适用范围与安全边界

- 只接受并默认信任官方仓库 `https://github.com/937204197/steady-relay`。用户提供
  其他仓库时，先说明这不是已验证来源并要求用户明确确认；不要自动运行其他仓库中的
  README、脚本或二进制。
- 读取 GitHub Release 元数据和下载资产前，先向用户展示仓库、版本、平台、架构、
  文件名和目标目录，并在用户确认后再执行下载。
- 只使用 Release 资产中的 `SHA256SUMS`（或同等官方校验文件）验证压缩包；校验失败
  时停止，不解压、不运行。
- 不把 API Key、完整请求体、响应体或带凭据的 URL 写入命令、日志或状态文件。上游
  Base URL 由用户输入，通常以 `/v1` 结尾；不要替用户猜测或预置第三方地址。
- 不执行压缩包内未知脚本或任意远程命令。只启动经过校验的 `steady-relay` 可执行文件
  或仓库 Release 自带的 `start.sh`/`start.bat`。
- 默认只使用 `127.0.0.1` 监听；不要建议 `0.0.0.0`，因为代理没有本地访问鉴权。
- 所有展示给用户的命令必须使用安全的路径引用（路径可能包含空格），并说明命令作用。
  不要要求用户手动 `cd`；应根据实际解压目录生成可复制的命令。
- 任何无法确认的仓库、版本、校验值、平台、监听端口或配置目标都应暂停并向用户说明，
  不要猜测继续。

## 触发和初始检查

当用户发送 GitHub 地址或说“安装/下载/启动 Steady Relay”时：

1. 解析 URL 的仓库所有者和名称。只有 `937204197/steady-relay` 自动进入安装流程。
2. 检测当前系统（macOS、Windows、Linux）和架构（`darwin-amd64`、`darwin-arm64`、
   `windows-amd64`、`windows-arm64`、`linux-amd64`、`linux-arm64`）。可以使用只读命令
   （例如 `uname -s`、`uname -m`，或 Windows PowerShell 的 `$env:PROCESSOR_ARCHITECTURE`）
   检查，不要把检测结果写入敏感文件。
3. 如果用户没有指定版本，读取官方仓库的最新 Release；如果最新版本没有当前平台资产，
   停止并告知用户可选资产或请其指定版本。不要从源码构建作为默认替代。
4. 向用户先报告：仓库、版本、选择的资产、校验文件、下载目录和将执行的下一步，等待确认。

仓库地址解析、Release 选择、下载、SHA-256 校验和安全解压应优先调用本 Skill 自带的
`scripts/release_resolver.py`，不要让模型手工拼接下载 URL 或自行实现校验。将下面的
`<skill-dir>` 替换为实际 Skill 目录；确认后可执行：

```bash
python3 <skill-dir>/scripts/release_resolver.py \
  "https://github.com/937204197/steady-relay" --json
```

在 Windows 上如果 `python3` 不存在，可依次尝试 `python` 或 `py -3`。这是 Codex 的辅助
运行环境，不要求用户在目标电脑上安装 Python；目标电脑只需要运行已下载的独立二进制。
脚本输出中的 `directory`、`cd_command` 和校验值应作为后续步骤的事实来源。如果辅助运行时
也没有 Python，停止并说明无法安全完成自动下载，不要退回到未经校验的手工下载命令。

## 下载、校验和解压

### 选择资产

Release 资产命名通常类似：

```text
steady-relay-2.2.0-darwin-arm64.tar.gz
steady-relay-2.2.0-darwin-amd64.tar.gz
steady-relay-2.2.0-linux-amd64.tar.gz
steady-relay-2.2.0-linux-arm64.tar.gz
steady-relay-2.2.0-windows-amd64.zip
steady-relay-2.2.0-windows-arm64.zip
```

实际版本和文件名必须以 Release 页面/API 返回的资产为准，不要凭空拼接下载地址。优先
下载对应压缩包和 `SHA256SUMS`，并确认校验文件来自同一个 Release/tag。

### 目标目录

建议使用版本独立目录，避免覆盖旧版本：

- macOS/Linux：`~/Applications/steady-relay/<version>/<platform-arch>/`
- Windows：`%LOCALAPPDATA%\\SteadyRelay\\<version>\\<platform-arch>\\`

先创建并展示实际绝对路径。下载文件可以放在临时目录，校验通过后再解压到上述目录。
不要使用仓库根目录、系统目录或不明确的通配符作为解压目标。

### 校验

在 macOS/Linux 使用系统提供的 `shasum -a 256` 或 `sha256sum`，Windows 使用
`Get-FileHash -Algorithm SHA256`。将计算出的哈希与同一 Release 的 `SHA256SUMS` 精确
比对（忽略文件名排序差异，但不忽略哈希字符）。哈希不一致时删除或隔离未验证文件，
向用户报告失败原因并停止。

### 安全解压

解压前检查归档路径，拒绝包含绝对路径、`../` 或 `..\\` 的条目，防止路径穿越。解压后
确认目标目录中存在 `steady-relay`（macOS/Linux）或 `steady-relay.exe`（Windows），
并确认随包提供的启动脚本（如 `start.sh`/`start.bat`）属于同一目录。不要运行归档中额外
的未知文件。

## 生成用户可复制的启动步骤

把实际解压目录代入命令，并正确引用路径。先显示命令，再执行；用户明确要求“帮我启动”
时可在确认后代为执行。

### macOS/Linux

优先使用发布包的 `start.sh`，它会启动同目录的二进制：

```bash
cd "/实际解压目录"
./start.sh
```

若脚本没有执行权限，向用户解释后执行（或提供）：

```bash
chmod +x "/实际解压目录/start.sh" "/实际解压目录/steady-relay"
```

macOS 首次被 Gatekeeper 拦截时，引导用户 Control-click/右键 `install-macos.command` →
“打开”→“打开”，或到“系统设置 → 隐私与安全性”点击“仍要打开”。不要建议关闭
Gatekeeper 或执行来源不明的绕过命令。

### Windows

优先生成 PowerShell 的安全路径命令：

```powershell
Set-Location -LiteralPath "C:\\实际解压目录"
Start-Process -FilePath ".\\start.bat"
```

也可让用户直接双击 `start.bat`。脚本会在没有 `UPSTREAM_BASE_URL` 时提示粘贴上游地址；
不要要求用户手动执行 `cd`。若使用旧版脚本没有交互提示，可在该文件夹地址栏输入 `cmd`，
然后执行：

```bat
set UPSTREAM_BASE_URL=https://你的上游地址/v1
start.bat
```

SmartScreen 提示时，先让用户确认下载来源确为官方 Release，再选择“更多信息 → 仍要运行”。

## 询问上游地址并启动

在启动前询问：

> 请输入你信任且有权使用的 OpenAI 兼容 API Base URL（通常以 `/v1` 结尾）。

只接受 `http://` 或 `https://`，并尽量检查路径是否包含 `/v1`。API Key 继续保存在
Codex/CC Switch 中，不要求用户把 Key 粘贴给向导，也不要把 Key 放在命令参数中。可使用
发布包脚本的交互提示；如需生成命令，优先使用环境变量或 `--upstream`，并提醒带 URL 的
命令可能进入 shell 历史记录：

```bash
UPSTREAM_BASE_URL="https://example.com/v1" ./start.sh
```

```powershell
$env:UPSTREAM_BASE_URL = "https://example.com/v1"
Start-Process -FilePath ".\\start.bat"
```

启动后读取终端日志，不要仅凭进程存在判断成功。记录日志显示的实际监听地址；默认是
`http://127.0.0.1:8080`，若端口被占用程序会尝试后续端口。使用日志中的实际端口，不能
继续假设为 8080。

## 健康检查和成功判定

找到日志中的本地端口后，请求：

```text
http://127.0.0.1:<实际端口>/healthz
```

只有健康检查成功、日志显示正在监听且进程仍在运行时，才向用户报告“启动成功”。向用户
显示本地 API Base URL：

```text
http://127.0.0.1:<实际端口>/v1
```

如果健康检查失败，先检查端口、上游 URL 和系统防火墙，再停止流程；不要让用户把未确认的
地址写入 Codex。

## CC Switch / Codex 配置（第一阶段默认手动）

第一阶段不要直接编辑 CC Switch 数据库，也不要静默改写 Codex 配置。启动成功后，引导用户：

1. 打开 CC Switch，进入要给 Codex 使用的供应商编辑页面。
2. 在“API 请求地址”栏填写刚才显示的本地地址，例如
   `http://127.0.0.1:8081/v1`；保留末尾 `/v1`。
3. 点击“应用”或“保存”。
4. 完全退出并重新启动 Codex。
5. 发起一次请求，并观察 Steady Relay 窗口是否出现
   `[request] ... forwarding`。

明确告诉用户：CC Switch 中填写本地 Steady Relay 地址，真实远程上游地址只在 Steady Relay
启动时输入；API Key 仍按 CC Switch/Codex 原方式保存。Steady Relay 的终端窗口或进程
必须持续运行，关闭它后 Codex 指向 `127.0.0.1` 的请求会失败。

## 状态记录、停止和恢复提醒

安装向导完成后，建议在安装目录写入不含 API Key 的状态文件（例如
`.steady-relay-session.json`），只记录：版本、绝对目录、PID（如能可靠获取）、本地 Base URL、
启动时间、用户原来使用的配置方式（CC Switch/直接 Codex）。不要记录上游 URL 中的凭据。

每次报告成功后同时提醒：

> 不使用 Steady Relay 时，先停止对应程序，再把 CC Switch/Codex 的 API 请求地址改回原来的
> 远程地址；否则下次 Codex 请求仍会指向已关闭的 `127.0.0.1`。第一阶段不自动恢复 CC Switch，
> 需要用户手动改回并重新启动 Codex。

用户要求停止时，先确认要停止的进程属于 Steady Relay（不要按模糊名称杀进程），再提供
`Ctrl+C`/关闭窗口等平台对应操作，并再次显示恢复配置提醒。若用户没有记录原地址，先让其
从 CC Switch/Codex 当前供应商配置中确认，不要猜测。

## 常见失败处理

- 找不到匹配资产：报告当前系统/架构与 Release 提供的资产，请用户选择或指定版本。
- SHA-256 不匹配：停止并重新下载；不要运行未验证文件。
- 端口占用：使用启动日志给出的新端口，并把新端口填入 CC Switch。
- Base URL 缺少 `/v1`：提醒补上；否则 Codex 可能请求到错误路径并收到 404。
- macOS 权限/隔离提示：按上面的首次打开流程处理，不关闭系统安全功能。
- 上游 DNS、网络或 VPN 错误：只报告错误并建议先验证上游地址；不要擅自改固定 IP、代理或
  系统 DNS。
- Codex 请求仍失败：先确认 Steady Relay 进程、`/healthz`、CC Switch 地址和日志中的
  `[request] ... forwarding`，再让用户提供去除 Key/正文后的日志片段。
