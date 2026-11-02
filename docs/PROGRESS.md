# Progress log

Newest first. Coordinator updates this at each gate.

## 2026-11-01 — Phase 2 (views, agent path, ops): gate passed

- **E, browse & detail**: All items (search first, URL-synced filters/sort,
  facets sheet, infinite scroll + Load more, scroll restore) and Item detail
  (Where-it-is panel with Move/Stage/Unstage + undo, role-tabbed gallery with
  uploads and cover, verbatim observations with `[?]` highlighting,
  identifiers, What-we-know with provenance sheet, correct/reject/add fact,
  product, references, sets, history).
- **F, places, packing, inbox, quick add**: location tree, box pages (photos,
  contents, staged, history, edit/archive, Move box, multi-select), printable
  labels with a self-contained QR encoder (4×6, A6, sheets of six), packing
  (pack into existing or new box in one move, undo), inbox (bulk upload with
  per-photo notes, assign / create item / delete, review accept), quick add.
- **G, agent ingest**: `POST /api/ingest/bundle` (validate-all 422, one
  transaction, idempotent via derived ext_keys, dry run), `inv ingest`
  (sha256 + HEAD, uploads only missing files, sidecar context), export
  (tar and `inv export`) / `inv import` with a byte-identical round trip,
  `docs/agents/INGEST_PLAYBOOK.md` and `ENRICH_PLAYBOOK.md` (examples are
  tested).
- **H, ops & budgets**: `inv backup` (VACUUM INTO, `--keep`), zero-write
  soak test (Go, in `make test`) and `scripts/soak.sh`,
  `scripts/measure-memory.sh`, CI lint job (actionlint + shellcheck) and an
  amd64 smoke test before push, `docs/DEPLOY.md` (TrueNAS custom app).

Measured (production image, arm64 Docker Desktop):

| | Measured | Budget |
|---|---|---|
| Idle RSS after restart | 14.8 MB | < 40 MB |
| RSS after 15.7k browse requests | 25.6 MB | < 40 MB |
| 48 MP HEIC peak, app + vips | 90.4 MB | < 150 MB |
| 48 MP JPEG peak, app + vips | 70.5 MB | < 150 MB |
| Writes while idle 45 s / browsing 1 min (15,708 requests) | 0 B, no file changed | 0 |

Gate checks by the coordinator:

- `make test`: every Go package (incl. ingest round trip, backup, soak),
  svelte-check 0/0, vitest 81 tests. actionlint and shellcheck clean.
- `scripts/soak.sh` against the rebuilt production container: PASS.
- `inv ingest` of the fixture folder against the container: first run
  uploads 4 photos and creates 3 items; second run uploads nothing and
  reports everything unchanged. `GET /api/export` streams all tables + originals.
- Multi-arch: thread H built linux/arm64 (89 MB) and linux/amd64 (102 MB)
  separately and ran the amd64 image under emulation (health, seed, backup).
  The combined buildx push runs in CI only (not run yet: nothing pushed).

Coordinator decisions in Phase 2:

- Generated TS types no longer treat request fields with defaults as
  required (`--default-non-nullable false`).
- Contract: dry-run bundles treat unknown sha256 as a warning.
- Accepted: G's re-apply rules (bundle `review_state` only when the item sets
  it; observations append-only; facts superseded on change; rejected facts
  never revived), F's QR-by-id labels and literal black-on-white print.

Known gaps / follow-ups:

- `TestZeroWriteSoak` failed twice inside a parallel `go test ./...` early
  on and then passed 9 runs in a row (alone and in parallel); not reproduced,
  cause unknown. The container soak passes.
- Refactors proposed by G: export shared helpers from items/media instead of
  local copies in ingest; an identifier_scheme "unique" flag instead of a
  hardcoded company-scheme list.
- Icons `print`, `camera`, `trash` for the kit.
- Re-applying an old bundle overwrites a title the owner has since edited
  (title is required in BundleItem).
- iOS Safari HEIC upload still unverified on a real phone.

## 2026-11-01 — Phase 1 (backend domains + UI kit): gate passed

Threads ran as parallel subagents, each in its own directory. The
coordinator re-ran every package's tests and checked the full stack in the
production container.

- **A, items** (15 tests): CRUD with nested create, observations,
  identifiers (normalization + lookup), facts with supersede/reject and the
  projection rule, products, tags, FTS5 search (prefix AND + exact
  identifier), all filters, facets (each group excludes its own filter),
  keyset paging, `/api/schema` + registry PUTs.
- **B, media** (22 tests): streamed upload with sha256 dedupe, magic-byte
  sniffing, EXIF subset (no GPS), worker (concurrency 1), variants, serving
  (immutable, ETag/304, Range, HTML sandbox CSP), links, inbox,
  `media.Rebuild`. Real HEIC fixtures in `internal/media/testdata/`.
  **48 MP HEIC peak: 90 MB** (vips child 69 MB + Go 20 MB), via banded
  decoding of the HEIC tile grid (`heif.go`); plain vipsthumbnail peaks at 239 MB.
- **C, locations** (13 tests): tree with direct/total/staged counts,
  create/patch/archive, item and whole-box moves (no per-item writes),
  packing, staging, movement log, placement derivation.
- **D, UI kit**: PhotoGrid, ItemCard, LocationChip, Gallery + MediaViewer,
  FilterSheet + ActiveFilters, LocationPicker, MultiSelectBar, states and
  primitives, on `/dev/kit` with fixtures and a live-data toggle.
  svelte-check 0/0, vitest 24 tests.

Gate checks by the coordinator:

- `make test` passes: all Go packages, svelte-check, vitest.
- Container with the demo seed: list/search/facets/locations/staging/inbox
  all serve real data. A HEIC upload is stored byte for byte (sha256
  matches), rotated (240×320), keeps `taken_at`, drops GPS; a re-upload
  returns 200 and writes nothing; thumbs serve with immutable caching.
- An item packed into a box in Unit 2 stays staged for Unit 1, then shows
  placed at "Unit 1 › B-002" once the box moves to Unit 1.
- Idle RSS 21 MB.

Coordinator decisions in Phase 1:

- **Placement:** an item whose location is its target or inside it is not
  staged (`locations.Within`); the items status filter matches.
- **Media dedupe:** a duplicate upload still applies `link_*` and fills an
  empty `context` (contract text updated).
- `inv media rebuild` wired to `media.Rebuild`.
- Toast host mounted once in the layout; viewer tokens
  (`--color-viewer-bg/fg`) keep the photo viewer dark; `createLocation`
  client helper.
- Accepted as-is: A's ten contract interpretations, C's archive rule (only
  owned staged items block archiving) and nesting rule (site/room/unit can't
  sit in a shelf or container), B's 400/422 choices.

Known gaps carried forward:

- A large *single-image* (non-grid) HEIC, or a grid with a crop box, still
  decodes whole. iPhone photos are tile grids, so the common case is
  covered; check with real photos in Phase 3.
- Whether iOS Safari uploads HEIC originals is unverified on a real phone
  (the upload input must not restrict `accept` to JPEG).
- `GET /api/locations/{id}` returns all contents with no paging.
- Media EXIF for HEIC has no `Orientation` (vips removes it after rotating).

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
