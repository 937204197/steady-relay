# Contributing

Thanks for contributing.

## Before opening a pull request

1. Keep the proxy transparent: do not rewrite request bodies, model names, API
   keys, or response bodies.
2. Preserve the safety rule that a stream with real output must never be
   replayed automatically.
3. Do not commit API keys, real request bodies, private upstream URLs, local
   configuration, or generated `dist/` artifacts.
4. Add or update tests for behavior changes.

Run the required checks from the repository root:

```bash
gofmt -w main.go main_test.go
go test ./...
go vet ./...
python3 -m unittest -v
```

## Pull request scope

Explain the user-visible behavior, retry/duplication implications, tests run,
and any documentation changes. Keep unrelated formatting changes separate.

## Releases

Maintainers build release archives with `scripts/build-release.sh VERSION`,
verify `SHA256SUMS`, then upload the artifacts to a GitHub Release. Binaries
and hashes must be produced from the tagged source revision.
