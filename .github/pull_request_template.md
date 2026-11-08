## 📚 Description

<!-- What changed, and why. Two or three sentences beat a changelog dump. -->

<!-- Call out anything that changes the wire format, routes, or the
     paper-only / T7 safety posture. -->

### Related ticket/PRs

<!-- Plane ticket, or the PR this depends on / unblocks. -->

## 🖼️ Visuals

<!-- Required for UI changes: before/after screenshots at desktop (1280px)
     and mobile (390px). Drag the images in, or link docs/screenshots/.
     Say "none" for non-UI changes. -->

## 🔍 Types of Changes

- [ ] Feature (new panel, route, or view state)
- [ ] Fix (rendering, data derivation, escaping)
- [ ] Refactor (no behaviour change)
- [ ] Chore (tooling, docs, pre-commit, CI)

## 🧪 Testing

<!-- What you ran, and what it printed. Not "tested locally". -->

```sh
templ generate
go build ./...
go test ./...
```

## ✅ Checklist

- [ ] `templ generate` output committed in the same change as the `.templ` edit
- [ ] Paper-only scope intact: sample fixture data only, no live venue calls
- [ ] T7 (arm/kill) still gated and inert — no handler, no state mutation, no persistence
- [ ] No secrets, tokens, or account IDs in the diff
- [ ] Docs updated (`README.md`, `docs/dev/lsp-setup.md`) when setup or routes change

## ⚠️ Caveats / Follow-ups

<!-- Known gaps, deferred work, or risk the reviewer should weigh. "None" otherwise. -->
