## Summary

Describe the user-visible change.

## Safety impact

- [ ] Request and response body forwarding remains transparent.
- [ ] A stream with actual output is not automatically replayed.
- [ ] No credentials, private URLs, prompts, or generated release files are included.

## Verification

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `python3 -m unittest -v`
