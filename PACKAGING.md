# 发布打包

正式发布使用静态编译的 Go 程序，目标电脑无需安装 Python、Go 或第三方依赖。

```bash
go test ./...
go vet ./...
python3 -m unittest -v
./scripts/build-release.sh 2.0.0
```

脚本会在 `dist/` 生成 macOS、Linux、Windows 的 amd64/arm64 六个压缩包，以及
`SHA256SUMS`。发布时将这些文件作为 GitHub Release 附件上传；不要将 `dist/` 提交到
源码仓库。

发布前检查：

1. 在干净环境中解压并运行一个目标包。
2. 使用显式 `--upstream https://api.example.com/v1` 或环境变量测试启动。
3. 核对启动日志中的版本、实际监听地址与上游地址。
4. 在 `dist/` 中运行 `shasum -a 256 -c SHA256SUMS`。
5. 写明变更内容、已知限制，以及未签名二进制的信任说明。

公开发布前建议配置 Apple Developer ID 签名/公证和 Windows Authenticode 签名。不要将
任何上游地址、固定 IP、API Key 或本地配置作为公开发布包的默认值。
