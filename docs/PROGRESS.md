# Progress log

Newest first. Coordinator updates this at each gate.

## 2026-11-01 — Phase 0 (foundation): gate passed

Done:

- Repo, `.gitignore`, `CLAUDE.md`, `Makefile` (targets delegate to the dev
  container when Go is missing on the host).
- Dev container: `golang:1.26-alpine3.22` + Node 22 + vips 8.16 / libheif
  1.19 (HEIC decode via libde265, encode via x265 for fixtures).
  Go 1.26, not 1.23: current `modernc.org/sqlite` (v1.61) requires it.
- Go skeleton: config, db (pragmas, `user_version` migration runner, ULIDs,
  error classifiers), httpapi (Deps, errors, middleware), model (shared JSON
  types), server (route wiring, `/api/health`, SPA), per-domain `Register`
  stubs for every contract route, frozen cross-package read helpers
  (`items.Summaries`, `items.Reindex`, `locations.Refs/ResolveRef/DerivePlacement`,
  `media.Refs/CoverFor/GenerateVariants`).
- `migrations/001_init.sql`: full §4 schema (STRICT tables, FTS5) plus seeded
  registries (kinds, media roles with cover rank, observation sources,
  21 fact keys, 13 identifier schemes).
- `api/openapi.yaml`: complete for §6 with examples; Redocly lint passes.
- SvelteKit 2 + Svelte 5 static SPA (adapter-static → `internal/webui/dist`,
  precompressed), tokens.css, app shell (top bar / phone tab bar), token
  prompt, generated types + openapi-fetch client, route stubs per thread.
- `inv seed --demo`: 61 items, 17 locations, ~80 generated placeholder
  photos through vips, every placement status, observations/identifiers/facts.
- Dockerfile (88 MB image, non-root 568), `compose.yml` (8480, two volumes,
  `local` log driver), GitHub Actions (test in dev container → buildx
  amd64+arm64 → GHCR on main/tags). actionlint clean.
- Tests: migrations + zero-write reopen, placement rules, location refs,
  health/SPA/auth, demo seed. All pass.

Gate (verified by the coordinator):

- `docker compose up` serves the SPA (including deep links) and
  `/api/health` on :8480 (`schema_version 1`, `vips 8.16.1`).
- `make test`: Go vet + tests, svelte-check (0 errors, 0 warnings), vitest pass.
- actionlint clean; Redocly lint of `api/openapi.yaml` clean.
- `inv seed --demo` in the container: 61 items.
- Idle RSS after seeding: 17 MB (VmRSS); `docker stats` shows ~48 MiB
  because it includes page cache.
- Docker Desktop's disk was full; the owner approved raising its limit by
  10 GB (61,035 → 71,275 MiB).

Not yet verified: the multi-arch (amd64 + arm64) build, which runs in CI
and is checked in Phase 2 (thread H).

## Coordinator decisions (also in PLAN.md §12)

- Routes are wired in `internal/server` (not `internal/httpapi`) so domain
  packages can import httpapi helpers without a cycle.
- Placement: rooms count as **placed**; only `site`/`unit` are
  **approximate** (an item "in Unit 2" is expected in some box).
- `observation`, `identifier`, `fact`, `reference` have an optional unique
  `ext_key` so bundles re-apply idempotently; `item.gone_at` added for
  "Sold · 2026-03"; `movement.kind` (move/stage/unstage) records staging history.
- Moves are a single `POST /api/moves` with `action`; PATCH never changes
  location or parent, so history is always recorded.
- SvelteKit 2 (not 3) for stability; the SPA builds into `internal/webui/dist`
  because `web/` carries a `go.mod` that keeps the Go tool out of node_modules.
