## What

<!-- One or two sentences: what does this change do? -->

## Why

<!-- The problem it solves or the behavior it fixes. Link issues. -->

## Checklist

- [ ] `go test ./...` passes — the suite must stay hermetic, driving `httptest` stubs and reaching no real server
- [ ] `golangci-lint run` clean
- [ ] No new dependency (standard library only is a stated property of this module)
- [ ] A new scene field is either in `SceneFields` *and* present on the oldest supported server, or behind a `Supports` probe
- [ ] Docs updated if the surface changed (README, docs/usage.md, docs/design.md)
