# Personal Inventory — MVP Plan

Status: **v2, Phase 0 complete** (2026-11-01). Decisions are in
§12. Items marked *(assumed)* are defaults I picked; they can change.

## 1. What this is

A self-hosted inventory of the things I own: what each thing looks like,
what's literally written on it, what we've learned about it, and **where it is
right now** (bedroom, staged to go to storage, in box B-014 in Unit 2, or
"somewhere — unknown"). It runs as one small container on my Mac or my TrueNAS
box.

### User stories, in priority order

1. **"Where's that jacket?"** Find an item by search or by browsing photos,
   see what it looks like, and see which unit and box it's in.
2. **"What's in this box?"** Open a box and see its contents as photos.
3. **"I'm packing."** Stage items to go to storage, then pack them into a
   box with one bulk action. At the unit, moving a box moves everything in it.
4. **"Agent, ingest these photos."** An LLM session takes a folder of photos,
   groups them into items, transcribes tags word for word, and writes
   everything to the inventory. I review it in the UI.
5. **"What exactly is this?"** Keep the raw label data (codes, fabric
   blends, RN numbers) apart from what we inferred or looked up (product
   name, season/year, stock photos, retail price), with provenance. Facts
   can be refined later without losing what was observed.

### Non-goals for the MVP

- Text extraction from images inside the app. Agents do that and write
  through the API.
- Server-side web scraping and email receipt parsing. The data model
  supports both; agents push the results for now.
- Storage-unit map visualization. The schema reserves room for it (§4.4).
- Multiple users, sharing, or public internet exposure.

## 2. Constraints and budgets

| Constraint | Budget / rule | How we verify |
|---|---|---|
| Memory | **< 40 MB RSS idle, < 150 MB peak** while processing a photo | `docker stats` in the ops workstream; Go `GOMEMLIMIT` |
| Disk writes (cheap SSD) | **Zero writes when idle and zero writes on read-only browsing.** Each upload is written once and never rewritten. | Idle and browse soak test checks file mtimes and `/proc/<pid>/io` write_bytes |
| Image fidelity | Originals are stored **byte for byte** and never re-encoded | sha256 of stored file equals sha256 of upload |
| Portability | One container image, linux/amd64 and linux/arm64 | GH Actions buildx |
| Data longevity | Everything exports to plain JSONL plus a media folder that you can read without the app | export → wipe → import round-trip test |

### Write-minimization rules (they apply to every workstream)

- SQLite: `journal_mode=WAL`, `synchronous=NORMAL`, `temp_store=MEMORY`,
  `mmap_size` set for reads, default autocheckpoint. One DB file in `/data`.
- **No writes on reads.** No view counters, no "last opened", and no server
  sessions table (auth is stateless, §6).
- Image variants are generated **once at ingest** and kept. They are not an
  evictable cache, so they never churn. `inv media rebuild` regenerates them
  if the variant spec changes.
- Uploads stream to a temp file in the destination directory, then
  `rename()`. There's no double copy through `/tmp`.
- Content-addressed media (sha256) means re-uploading the same photo writes
  nothing.
- Logs go to stdout at `info` level and access logs are off by default. The
  compose file sets the Docker log driver to `local` with `max-size: 1m`,
  because the default `json-file` driver writes unbounded logs to disk.
- Responses carry `Cache-Control: immutable` for media, so the browser does
  the caching.
- Two volumes: `/data` on the NAS SSD app pool holds the DB, config, and
  **thumbnails**. `/media` on the HDD pool holds originals, display
  variants, and archives. Thumbnails are small (~30 KB each, ~100 MB for
  thousands of items) and written once. Keeping them on the SSD means
  browsing the grid doesn't spin up the HDDs; the disks spin only when
  you open a photo full size.

## 3. Architecture and stack

```
┌──────────── container (one process) ────────────┐
│  inv  (Go binary)                                │
│   ├─ HTTP: /api/* JSON, /media/*, SPA (embedded) │
│   ├─ SQLite (modernc, pure Go) ── /data/inv.db   │
│   ├─ media pipeline ── exec libvips CLI ── /media│
│   └─ CLI: serve | ingest | export | import |     │
│           media rebuild | backup | seed          │
└──────────────────────────────────────────────────┘
       ▲ browser (phone/desktop)     ▲ agents (HTTP API / `inv` CLI)
```

| Choice | Pick | Why |
|---|---|---|
| Backend | **Go 1.23+, stdlib `net/http`** (1.22 pattern routing) | ~10–20 MB idle RSS, a single static binary, fast cold start. Node idles at 50–80 MB. |
| DB | **SQLite** via `modernc.org/sqlite` + FTS5 | One file, no server, no cgo. Thousands of items is tiny. |
| Images | **libvips CLI** (`vipsthumbnail`, `vipsheader`) with **libheif** | Streaming and low-memory, decodes iPhone **HEIC**, auto-rotates from EXIF. It runs only during processing, so it uses no resident memory. |
| Frontend | **Svelte 5 + Vite, static SPA**, embedded with `go:embed` | Small bundles, and it doesn't need a Node server at runtime |
| Styling | Plain CSS with **design tokens** (CSS custom properties in one file) | A future design pass can restyle the app without touching component logic |
| API contract | **OpenAPI 3.1** at `api/openapi.yaml`, TS types generated by `openapi-typescript` | The backend stays independent of the design, and parallel workstreams code against one contract |
| IDs | ULIDs (text), plus human short codes for boxes and units | Stable across export/import, sortable |
| Image base | `alpine` + `vips-tools vips-heif` | Small, and has HEIC support |

Go isn't installed on this Mac. All building and testing happens in the dev
container (`.devcontainer/`, with Go, Node 22, and libvips), following the
same pattern as `twMediaDownloader`.

## 4. Data model

The core idea is three layers per physical item:

| Layer | Table | Meaning | Mutability |
|---|---|---|---|
| **Observed** | `observation`, `identifier` | What is *literally on the thing*: tag text verbatim, plus an as-read structure | Append-only. Corrections are new rows. |
| **Derived** | `fact` | What we *believe*: inferred, looked up, or user-asserted, each with confidence and sources | Append-only with supersede chains |
| **Projection** | `item.attrs` (JSON) | The current best value per key, computed from facts | Recomputed on write; it's a cache column |

Separately, a **`product`** is the catalog-level thing ("Uniqlo AIRism
Crew Neck T, 2023 version"), while an **`item`** is *my physical
instance*. Stock photos, retail price, and release season belong to the
product. Size, condition, purchase price, and location belong to the item.
Two of my shirts can point at one product, and five yearly versions of
"the same shirt" are five products. `product_id` is optional, so items work
fine without one.

### 4.1 Tables (sketch; Phase 0 finalizes them in `migrations/001_init.sql`)

```sql
item (
  id TEXT PK,                 -- ULID
  kind TEXT NOT NULL,         -- 'clothing' | 'object' | 'utility'
  title TEXT NOT NULL,        -- "Navy Gore-Tex shell jacket"
  category TEXT,              -- freeform path within kind: 'outerwear/shell', 'cable/usb-c'
  product_id TEXT NULL → product,
  parent_item_id TEXT NULL → item,    -- sets: suit = jacket + trousers
  quantity INTEGER DEFAULT 1,         -- "4× USB-C cable 1m"
  location_id TEXT NULL → location,        -- deepest KNOWN location; NULL = unknown
  target_location_id TEXT NULL → location, -- non-null = staged / queued to go there
  lifecycle TEXT DEFAULT 'owned',     -- 'owned' | 'gone'
  gone_reason TEXT NULL,              -- sold | donated | discarded | lost
  review_state TEXT DEFAULT 'ok',     -- 'ok' | 'needs_review' (agent-created)
  cover_media_id TEXT NULL,
  notes TEXT,
  attrs JSON NOT NULL DEFAULT '{}',   -- projection of active facts
  ext_key TEXT UNIQUE NULL,           -- idempotency key supplied by agents
  created_at, updated_at
)

product (id, brand, name, attrs JSON, notes, ext_key, created_at, updated_at)

observation (                    -- RAW: what's written on the thing
  id, item_id → item,
  source TEXT,      -- garment_label | care_label | hang_tag | print | packaging
                    -- | receipt | user_note | other  (freeform, registry-suggested)
  media_id NULL,    -- photo it was read from
  region JSON NULL, -- optional bbox on that photo
  text TEXT,        -- verbatim transcription, line breaks preserved
  data JSON,        -- as-read structure, no inference (see example below)
  recorded_by TEXT, -- 'agent:claude-opus-5-5' | 'user'
  recorded_at
)

identifier (                     -- any code, any scheme, indexed for lookup
  id, subject_type ('item'|'product'), subject_id,
  scheme TEXT,      -- freeform: 'uniqlo.product_code', 'rn', 'upc', 'style', 'ca'
  value TEXT, value_norm TEXT,   -- normalized (uppercase, no spaces/dashes) for matching
  observation_id NULL,           -- where it was read
  note TEXT
)  INDEX (value_norm), INDEX (scheme, value_norm)

fact (                           -- DERIVED: claims with provenance
  id, subject_type, subject_id,
  key TEXT,         -- dotted, freeform: 'brand', 'color', 'size', 'season',
                    -- 'composition', 'shell.technology', 'retail_price'
  value JSON,
  basis TEXT,       -- 'user' | 'observed' | 'looked_up' | 'inferred'
  confidence REAL,  -- 0..1
  sources JSON,     -- [{observation_id}|{reference_id}|{url}|{note}]
  status TEXT,      -- 'active' | 'superseded' | 'rejected'
  supersedes_id NULL,
  recorded_by, recorded_at
)

reference (                      -- external sources (product pages, listings, receipts)
  id, subject_type, subject_id, url, title,
  kind TEXT,        -- product_page | listing | review | receipt | other
  archive_media_id NULL,  -- archived snapshot (html/pdf/txt), because pages disappear
  fetched_at, notes
)

media (                          -- every blob: photos, archived pages, receipts
  id, sha256 UNIQUE, mime, bytes, width, height,
  taken_at NULL,    -- EXIF DateTimeOriginal (helps agents group bursts)
  original_name, exif JSON (subset, no GPS),
  context TEXT NULL, -- owner's free-text note about the photo ("bought in Tokyo 2019,
                     -- the grey one not the navy"); agents treat it as user-asserted
  created_at
)
media_link (media_id, subject_type, subject_id, role, sort, caption)
  -- role: flat_lay | on_body | stock | label | care_label | hang_tag | detail
  --       | defect | box_exterior | box_contents | receipt | location | other
  -- a media row with no links = "Inbox"

location (
  id, code TEXT UNIQUE NULL,  -- printed label: 'U2', 'B-014'
  name, kind TEXT,  -- 'site' | 'room' | 'unit' | 'shelf' | 'container'
  parent_id NULL → location,
  cover_media_id, notes,
  layout JSON NULL,   -- FUTURE: map/floorplan + this node's position within parent
  archived_at NULL, created_at, updated_at
)

movement (id, item_id NULL, location_moved_id NULL,  -- an item move or a whole-box move
          from_location_id, to_location_id, at, note, actor)

tag (item_id, tag)

-- registries: vocabulary that grows as the model matures; unknown values still work
fact_key (key PK, label, value_type, kinds, filterable BOOL, description)
identifier_scheme (scheme PK, label, brand, description, pattern, example, notes)

item_fts  -- FTS5 over title, notes, category, tags, observation text,
          -- identifier values, selected attrs; maintained by app code on write
```

### 4.2 Raw vs. derived, worked example

A care-label photo of a jacket gives:

```jsonc
// observation (RAW: what's printed; normalized into structure, nothing inferred)
{ "source": "care_label", "media_id": "…",
  "text": "SHELL: 100% NYLON\nLINING: 100% POLYESTER\nRN 12345\nMADE IN VIETNAM\n341-456789(23SS)",
  "data": { "composition": [ {"part":"shell","materials":[{"name":"nylon","pct":100}]},
                             {"part":"lining","materials":[{"name":"polyester","pct":100}]} ],
            "country_of_origin": "VIETNAM",
            "codes": ["RN 12345", "341-456789(23SS)"] } }

// identifiers extracted from it
{ "scheme": "rn", "value": "12345" }
{ "scheme": "uniqlo.product_code", "value": "341-456789" }
{ "scheme": "uniqlo.season_code", "value": "23SS" }

// facts (DERIVED), each with provenance
{ "key": "composition", "basis": "observed", "confidence": 1.0, "value": […], "sources": [{"observation_id":"…"}] }
{ "key": "season", "basis": "inferred", "confidence": 0.8, "value": "2023 Spring/Summer",
  "sources": [{"observation_id":"…"}, {"note":"Uniqlo '23SS' suffix convention"}] }
{ "key": "shell.technology", "basis": "looked_up", "confidence": 0.6, "value": "GORE-TEX 3L",
  "sources": [{"reference_id":"…"}] }
```

**Projection rule** (`item.attrs`): for each key, take the active fact with
the best basis (`user` > `observed` > `looked_up` > `inferred`), then the
highest confidence, then the newest. Product facts fill in keys the item
doesn't have. The UI always shows the observed layer and the derived layer
separately, with provenance one tap away.

**How the model grows:** new keys and identifier schemes need no migration.
Each is a string in a row. When a key becomes useful for filtering, add it to
`fact_key` with `filterable = 1` and it appears in the filter UI.
`identifier_scheme.notes` is where accumulated knowledge goes, such as
"Uniqlo 3xx-xxxxxx = product code; suffix = season".

### 4.3 Location and placement semantics

"Where is it" combines two pointers with lifecycle:

| UI status | Condition | Display |
|---|---|---|
| **Gone** | `lifecycle='gone'` | "Sold · 2026-03" |
| **Staged** | `target_location_id` set | "Bedroom → going to Unit 2" |
| **Unknown** | `location_id` NULL | "Location unknown" |
| **Approximate** | location kind is `unit`/`site`/`room` but the item is expected in a container | "Somewhere in Unit 2 (box unknown)" |
| **Placed** | location kind is `container`/`shelf` | "Unit 2 › B-014" |

- Partial knowledge is natural: point at the deepest node you're sure of.
  "In Unit 2, not sure which box" means `location_id = Unit 2`.
- **Moving a box moves its contents** with no per-item writes, because items
  reference the box. One `movement` row records the box move.
- The **packing flow** clears `target_location_id` and sets `location_id` in a
  single bulk move.
- Every move appends to `movement`, which gives a history ("moved to B-014
  on 2026-11-02").

### 4.4 Future map, reserved now

`location.layout` will hold, for a unit, `{floorplan_media_id, width_cm,
depth_cm}`, and for a box, `{x, y, w, h, stack_level}` in its parent's
coordinates. There's no UI in the MVP. The column exists so the map feature
is additive.

## 5. Media pipeline

1. `POST /api/media` (multipart, streamed). The server hashes while
   writing. If the sha256 already exists, it discards the upload and returns
   the existing row.
2. The original is stored at `/media/orig/ab/cd/<sha256>.<ext>`, untouched,
   EXIF included.
3. A worker generates variants once (concurrency 1 to cap memory):
   `thumb` = 400px WebP q75, stored at `/data/thumbs/ab/<sha256>.webp`
   (SSD, see §2), and `display` = 1600px WebP q82, stored at
   `/media/var/ab/<sha256>.webp` (HDD). Both are auto-rotated and
   metadata-stripped.
4. `taken_at`, dimensions, and an EXIF subset (no GPS) are written to the
   `media` row.
5. Serving: `GET /media/<sha256>/{thumb|display|orig}` with immutable caching
   and `Content-Disposition` for originals.
6. Supported formats: JPEG, PNG, WebP, HEIC/HEIF, and PDF/HTML for
   `reference` archives (stored as-is, no variants). The max upload size is
   configurable (default 64 MB).
7. Unlinked media appears in the **Inbox** for later assignment.

## 6. API surface

The normative contract is `api/openapi.yaml`, written in Phase 0. This is
the outline:

```
GET    /api/health
GET    /api/schema                  kinds, roles, fact_keys, identifier_schemes (agent vocab discovery)
GET    /api/items?q=&kind=&category=&location=&status=&tag=&review=&attr.<key>=&sort=&cursor=
POST   /api/items                   (accepts nested observations/identifiers/facts/media links)
GET    /api/items/:id               (full: media, observations, identifiers, facts, refs, movements, product)
PATCH  /api/items/:id
DELETE /api/items/:id               soft: lifecycle → gone; hard delete is a separate admin route
POST   /api/items/:id/{observations|identifiers|facts|references|tags}
POST   /api/facts/:id/{supersede|reject}
GET    /api/facets                  counts by kind/category/brand/color/size/location/status
GET    /api/identifiers/lookup?value=   duplicate detection before creating
CRUD   /api/products
GET    /api/locations               tree with item counts
CRUD   /api/locations/:id           GET includes contents
POST   /api/moves                   {item_ids[] | location_id, to_location_id | target_location_id, note}
GET    /api/staging                 staged items grouped by target
POST   /api/media                   multipart upload
HEAD   /api/media/sha256/:sha       "already have it?"
POST   /api/media/:id/links
GET    /api/inbox                   unlinked media + needs_review items
POST   /api/ingest/bundle           transactional, idempotent bulk write (see §7)
GET    /api/export                  streams a tar (jsonl + media); CLI equivalent exists
```

**Auth** *(assumed)*: off by default for LAN use. If `INV_TOKEN` is set,
`/api` writes require `Authorization: Bearer`. The UI asks for the token
once and keeps it in localStorage. Nothing is stored server-side.

## 7. Agent ingest (the near-term workflow)

The goal: point an LLM session at a folder of photos and get reviewed
records.

**Expected input** (owner's actual workflow): iPhone HEIC photos from the
camera roll, mixed with other shots, usually **1–2 photos per garment**,
with optional per-photo context from the owner. Tags often won't be
photographed, so the observed layer will be sparse for many items. That's
expected, and items can be created **with no photos at all**; the UI shows
a kind/category placeholder. There are two ways in:

1. **Inbox-driven** (preferred day-to-day): upload photos to the Inbox from
   the phone or desktop, adding a context note per photo if you want. An
   agent then reads `GET /api/inbox` (photos plus context), proposes item
   groupings, and writes them back with bundle semantics, linking the
   existing media by id. The agent works over the API, so photos never need
   to sit in a local folder for it. Note: iOS Safari may transcode HEIC to
   JPEG on upload. Phase 1 thread B checks this, and the upload input should
   request originals where the browser allows it.
2. **Folder-driven**: export originals from Photos to a folder (plus an
   optional `context.txt` or `<photo>.txt` sidecar per image), then the
   agent builds a bundle and runs `inv ingest`.

**Ingest bundle.** The agent writes a JSON file. `inv ingest bundle.json
--server http://host:8480` uploads any photos the server doesn't have
(checked by sha256) and then POSTs the bundle. The write is transactional
and **idempotent via `ext_key`**: re-running updates instead of duplicating.
`--dry-run` prints what would change.

```jsonc
{
  "defaults": { "location": "code:BEDROOM", "target_location": "code:U2",
                "review_state": "needs_review", "recorded_by": "agent:claude-opus-5-5" },
  "items": [{
    "ext_key": "batch-2026-11-01/jacket-01",
    "kind": "clothing", "title": "Navy shell jacket", "category": "outerwear/shell",
    "media": [ {"path": "./IMG_4101.HEIC", "role": "flat_lay"},
               {"path": "./IMG_4102.HEIC", "role": "care_label"} ],
    "observations": [ {"source": "care_label", "media": "./IMG_4102.HEIC",
                       "text": "…verbatim…", "data": {…}} ],
    "identifiers": [ {"scheme": "rn", "value": "12345", "observation": 0} ],
    "facts": [ {"key": "brand", "value": "Uniqlo", "basis": "observed", "confidence": 1} ],
    "product": { "ext_key": "uniqlo/341-456789/23SS", "brand": "Uniqlo", "name": "…" }
  }]
}
```

Phase 2 deliverables:

- `docs/agents/INGEST_PLAYBOOK.md`: instructions any Claude session follows.
  It covers grouping photos into items (EXIF time bursts plus visual
  similarity), transcribing **verbatim and never guessing**, putting
  uncertain readings in `observation.text` with `[?]`, choosing scheme
  names (check `/api/schema` first, then reuse or extend), checking
  `/api/identifiers/lookup` before creating, and always setting
  `needs_review`.
- `docs/agents/ENRICH_PLAYBOOK.md`: the follow-up pass that finds the product
  page, archives it plus stock images as `reference` and `media` with
  `role=stock`, and adds `looked_up` facts.
- `inv export` / `inv import`: a directory of `items.jsonl`, `products.jsonl`,
  `locations.jsonl`, …, plus `media/`. It's readable without the app.
- A post-MVP option: a thin **MCP server** exposing the same operations.

## 8. Frontend

**Design direction.** Calm and photo-first, with neutral surfaces, one
accent color, system fonts, generous whitespace, and dark mode. Desktop and
phone layouts are equally important; at the storage unit, the phone is the
main device. Everything visual comes from `web/src/styles/tokens.css` so it
can be restyled later.

| View | Purpose | Key bits |
|---|---|---|
| **All items** (home) | "Where's that jacket?" | Search box first. A photo grid (cover = flat lay > stock > any), and each card has a location chip. A filter sheet for kind, category, brand, color, size, location, status, and tags, with facets from `/api/facets`. |
| **Item detail** | Remember it, then find it | A **"Where it is" panel at the top** with a Move action. The photo gallery is tabbed by role (Flat lay · On body · Stock · Labels · Detail). Then **"On the item"** (raw observations verbatim with the source photo crop, plus identifiers) and **"What we know"** (projected attrs; tap for provenance and history). Then product, references, purchase, and movement history. |
| **Locations** | "What's in this box?" | A tree of sites → units → boxes with counts. The box page shows its label code, photo, path, and contents grid, plus "Move box". |
| **Staging / packing** | "I'm packing" | Items grouped by target. Multi-select, then **Pack into box…** (pick or create a box) in one bulk move. Also an "Unknown location" list. |
| **Inbox / review** | Feeding agents, then reviewing their output | Bulk upload from the phone camera roll with an optional context note per photo. Unlinked photos (assign to an item or create one) and `needs_review` items with accept/edit actions. |
| **Quick add** | Manual escape hatch | Photos + kind + title + location. Everything else is optional. |
| **Box label** | Printable | Code, name, and a QR code linking to the box page *(nice-to-have)* |

## 9. Repo layout

```
cmd/inv/                main.go: CLI + serve
internal/db/            open, pragmas, migrations runner, tx helpers
internal/items/         items, products, observations, identifiers, facts, projection, search
internal/media/         upload, hashing, vips pipeline, serving, inbox
internal/locations/     tree, moves, staging
internal/ingest/        bundle apply, export/import
internal/httpapi/       router, auth middleware, errors, OpenAPI conformance tests
migrations/             001_init.sql …
api/openapi.yaml        the contract
web/                    Svelte SPA (src/lib/api generated from openapi.yaml)
docs/                   PLAN.md, API notes, agents/ playbooks, DEPLOY.md
.devcontainer/          Go + Node 22 + libvips/libheif
Dockerfile              multi-stage: web build → go build → alpine + vips
compose.yml             local run & TrueNAS custom-app shape
.github/workflows/      test + buildx multi-arch → ghcr.io
```

Each domain package exposes `Register(mux, deps)`. The route wiring and the
migration file are written once in Phase 0, so parallel workstreams never
edit the same files.

## 10. Workstreams for the coordinator

Rules for all threads: code against `api/openapi.yaml`, and propose any
contract change to the coordinator before making it. **Don't add
migrations in Phase 1–2**; schema changes go through the coordinator as
`00N_*.sql`. All tests run in the dev container. Every API change ships
with Go `httptest` tests against a real temp SQLite and real vips.

### Phase 0: Foundation (one thread, serial, blocks everything)

- Repo, `.gitignore`, `CLAUDE.md` (how to build, test, and run; the write
  rules from §2), dev container, `Makefile` (`dev`, `test`, `build`, `image`)
- Go skeleton: config from env, pragmas, migration runner,
  `migrations/001_init.sql` (the full §4 schema plus seeded registries),
  router with per-package stub `Register`s, `/api/health`
- `api/openapi.yaml`, complete for §6 with examples
- Svelte skeleton: routing, tokens.css, layout shell, generated API client,
  embedded via `go:embed`, Vite dev proxy
- `inv seed --demo`: about 60 fake items across kinds, locations, and
  states, with generated placeholder images, so the frontend has content
- Dockerfile, compose.yml (ports, two volumes, log driver), GH Actions
  workflow (test, then buildx amd64+arm64, then push to GHCR on main)
- **Gate:** `docker compose up` serves the shell SPA and `/api/health` on
  :8480, `make test` passes, and the CI workflow file lints.

### Phase 1: Backend domains plus UI kit (4 threads in parallel)

| Thread | Owns | Done when |
|---|---|---|
| **A: Items** | `internal/items` | CRUD with nested create; observations, identifiers (normalization + lookup), facts with supersede/reject and the projection rule; products; tags; FTS search; filters and facets; `/api/schema`. Tests cover projection precedence and search. |
| **B: Media** | `internal/media` | Streamed upload with dedupe, the vips pipeline including **a real HEIC fixture**, EXIF taken_at, variants, serving with cache headers, links, inbox, `inv media rebuild`. A memory test shows peak < 150 MB on a 48 MP HEIC. |
| **C: Locations** | `internal/locations` | Tree CRUD with codes, contents, bulk moves for items and boxes, staging, movement log, and correct placement-status derivation. Tests cover box-move semantics. |
| **D: UI kit** | `web/src/lib/components` | Photo grid, item card + location chip, gallery with role tabs, filter sheet, location picker (tree + search + create), multi-select bar, empty/loading states, light and dark, all on a `/dev/kit` page against the demo seed |

### Phase 2: Views plus agent path plus ops (4 threads in parallel)

| Thread | Owns | Done when |
|---|---|---|
| **E: Browse & detail** | `web/src/routes/(items)` | The All items and Item detail views in §8 work against the real API, on phone and desktop |
| **F: Locations, packing, inbox** | `web/src/routes/(locations, staging, inbox, add)` | Box pages, the packing flow, unknown list, inbox assign/review, quick add, box label page |
| **G: Agent ingest** | `internal/ingest`, `docs/agents/` | `POST /api/ingest/bundle` + `inv ingest` (upload by sha, idempotent, dry-run), export/import round trip, both playbooks. Tested by ingesting a fixture folder twice with no duplicates. |
| **H: Ops & budgets** | `docs/DEPLOY.md`, CI, soak tests | Measured idle and peak RSS, an idle/browse **zero-write** soak test, `inv backup` (`VACUUM INTO`), the TrueNAS custom-app guide, a GHCR image pullable on the NAS |

### Phase 3: Integration and first real data (coordinator plus owner)

- An end-to-end walkthrough: ingest → review → stage → pack → find → move box
- **Ingest the current clothing photos** using the playbook. Iterate on the
  playbook and registries based on what real tags look like.
- A polish pass on whatever felt clunky

## 11. After the MVP (not scoped)

- A storage-unit map: a floorplan photo or grid, box pins, stack levels,
  and "show me where" from an item
- Receipt hydration from email, run by an agent (purchase date, price, order
  number as observations and facts, plus the archived email)
- A server-side URL archiver for product pages and stock images
- An MCP server wrapping the API
- An offline-capable PWA packing list for units without LAN
- QR scanning from the phone camera to open a box
- A resale kit: export one item's stock and flat-lay photos, specs, and
  original retail price

## 12. Decisions and assumptions

Decided (owner, 2026-10-31):

- **Remote access is Tailscale.** The phone reaches the app at the unit, so
  the app stays LAN/tailnet-only and offline mode stays post-MVP. The
  phone layout is first-class.
- **Photos:** iPhone HEIC from the camera roll, mixed, 1–2 per garment,
  photos optional per item, with per-photo owner context (§7).
- **Repo and GHCR image are public.** The NAS already handles registry
  auth, so the deploy docs don't cover it.
- **Deploy target is the NAS from the start** (TrueNAS, amd64 assumed).
  `/data` and thumbnails go on the SSD app pool, and `/media` on the HDD
  pool. Local Docker on the Mac is for dev only.

Coordinator decisions (Phase 0, 2026-11-01; details in `docs/PROGRESS.md`):

- **Toolchain:** Go 1.26 (current `modernc.org/sqlite` requires it), Alpine
  3.22 for both the dev container and the runtime (Node 22, vips 8.16,
  libheif 1.19), SvelteKit 2 + Svelte 5.
- **Routes are wired in `internal/server`**, not `internal/httpapi`, so
  domain packages can use the httpapi helpers without an import cycle.
  Listing packages get item cards through `Deps.ItemSummaries`.
- **Placement:** rooms count as *placed*; only `site` and `unit` are
  *approximate* (an item "in Unit 2" is expected in some box there).
- **Schema additions to the §4 sketch:** `item.gone_at`; `movement.kind`
  (move / stage / unstage); an optional unique `ext_key` on observation,
  identifier, fact and reference so bundles re-apply idempotently;
  `media.variant_status`; registries for item kinds, media roles (with
  cover rank) and observation sources.
- **Moves** are one `POST /api/moves` with an `action`; PATCH never changes
  an item's location or a location's parent, so history is always recorded.
- **No Docker HEALTHCHECK**, to avoid daemon-persisted probe state.

Still assumed (object if you disagree):

- *(assumed)* Language is Go rather than Node, chosen for memory. Your
  other project uses Node, so say so if you'd rather stay consistent.
- *(assumed)* Port 8480, the binary/CLI is called `inv`, and the product
  name is "Personal Inventory" for now.
- *(assumed)* Quantities and sets are supported (`quantity`,
  `parent_item_id`). Socks and cables usually don't need per-unit records.
- *(assumed)* Gone items are kept forever, and hard delete is admin-only.
