# Personal Inventory — working notes for Claude sessions

The spec is `docs/PLAN.md`. The API contract is `api/openapi.yaml`. Progress
is logged in `docs/PROGRESS.md`. Read all three before changing anything.

## Build, test, run

Go is **not** installed on the Mac. Every `make` target runs inside the dev
container automatically (`.devcontainer/`: Go 1.26, Node 22, libvips 8.16 +
libheif on Alpine 3.22, the same Alpine as the runtime image).

| Command | What it does |
|---|---|
| `make deps` | `go mod download` + `npm ci` in `web/` (node_modules lives in a container volume) |
| `make test` | `go vet`, `go test ./...`, `svelte-check`, `vitest` |
| `make test-go` / `make test-web` | One side only |
| `make gen` | Regenerate `web/src/lib/api/schema.d.ts` from `api/openapi.yaml` (commit the result) |
| `make web` | Build the SPA into `internal/webui/dist` (embedded with `go:embed`) |
| `make run` | `inv serve` in the dev container on `127.0.0.1:8481` (data in the container's `.local/` volume) |
| `make dev` | Vite on `:5173` proxying `/api` and `/media` to the server |
| `make seed` | `inv seed --demo` into the dev database |
| `make up` / `make down` | Build and run the production image with `compose.yml` on `:8480` |
| `make lint` | actionlint on the CI workflow |
| `make shell` | Shell in the dev container |

Run one Go test: `docker compose -f .devcontainer/docker-compose.yml run --rm -T dev go test ./internal/items -run TestName -v`.

`go test` needs `internal/webui/dist/index.html` to exist; `make test`
creates a placeholder. Seed the running container with
`docker compose exec inv inv seed --demo`.

## Layout and ownership

```
cmd/inv/            CLI + serve (coordinator)
internal/config     INV_* env config
internal/db         open + pragmas, migrations, Tx, NewID, Now, IsUnique…
internal/httpapi    Deps, JSON errors, DecodeJSON, cursors, auth/recover/access-log middleware
internal/model      JSON types shared across packages (mirror openapi schemas)
internal/server     route assembly, /api/health, SPA mount — the only place routes are wired
internal/testutil   NewDeps (temp DB), Do (httptest JSON), Mtimes (zero-write checks)
internal/items      thread A      internal/media      thread B
internal/locations  thread C      internal/ingest     thread G
internal/seed       demo data     internal/webui      embedded SPA handler
migrations/         001_init.sql (schema + seeded registries)
web/src/styles      tokens.css (all visual values), base.css
web/src/lib/api     generated schema.d.ts + client.ts (typed openapi-fetch client)
web/src/lib/components  thread D (UI kit; shown on /dev/kit)
web/src/routes/(items)  thread E; (locations) (staging) (inbox) (add)  thread F
```

Rules for every thread:

- Touch only the package/directory you own. Each domain exposes
  `Register(mux, deps)`; the route list in each `register.go` matches the
  contract. Replace `httpapi.NotImplemented` with real handlers.
- **Frozen cross-package functions** (signatures fixed; owners may change
  internals): `items.Summaries`, `items.Reindex`, `locations.Refs`,
  `locations.ResolveRef`, `locations.DerivePlacement`, `locations.PathLabel`,
  `media.Refs`, `media.CoverFor`, `media.GenerateVariants`,
  `media.OrigPath/DisplayPath/ThumbPath/URL`.
- Packages that list items use `deps.ItemSummaries` instead of importing
  `internal/items` (avoids an import cycle). Import direction: media → (none),
  locations → media, items → locations + media, ingest → all.
- **No new migrations in Phases 1–2.** Schema or contract changes are
  proposed to the coordinator first.
- Every API change ships with `httptest` tests against a real temp SQLite
  (`testutil.NewDeps`) and real vips.
- Request bodies are decoded with `httpapi.DecodeJSON` (rejects unknown
  fields). Errors are `*httpapi.Error` → `{"error":{code,message,details}}`.
- Location references accept an id or `code:<CODE>` everywhere
  (`locations.ResolveRef`).
- Frontend: style only with `var(--…)` tokens. Phone layouts are first-class
  (storage unit, over Tailscale); minimum tap target `--tap-min`.

## Write-minimization rules (PLAN.md §2) — apply everywhere

The NAS SSD is cheap. **Zero disk writes when idle and on read-only browsing.**

- SQLite: WAL, `synchronous=NORMAL`, `temp_store=MEMORY`, mmap for reads,
  default autocheckpoint, one DB file in `/data`. Set in `internal/db`; don't
  add per-connection pragmas that write.
- **No writes on reads**: no view counters, "last opened", sessions table or
  caches written from GET handlers. Auth is stateless (`INV_TOKEN`).
- Image variants are generated **once at ingest** and kept (not an evictable
  cache). `inv media rebuild` regenerates them if the spec changes.
- Uploads stream to a temp file **in the destination directory**, then
  `rename()`. No double copy through `/tmp`.
- Media is content-addressed (sha256): re-uploading identical bytes writes nothing.
- Logs: stdout, `info` level, access log off (`INV_ACCESS_LOG=1` to enable).
  compose uses the `local` log driver with `max-size: 1m`. Don't log per request.
- Media responses carry `Cache-Control: public, max-age=31536000, immutable`.
- Storage split: `/data` (SSD) = `inv.db`, `thumbs/`; `/media` (HDD) =
  `orig/`, `var/` (display variants), archives. Grid browsing must touch only `/data`.
- No Docker HEALTHCHECK (daemon-persisted state); probe `/api/health` externally.

## Config (env)

`INV_ADDR` (`:8480`), `INV_DATA_DIR` (`/data`), `INV_MEDIA_DIR` (`/media`),
`INV_DB_PATH` (`$INV_DATA_DIR/inv.db`), `INV_TOKEN` (unset = no auth),
`INV_MAX_UPLOAD_MB` (64), `INV_LOG_LEVEL` (info), `INV_ACCESS_LOG` (0),
`INV_VIPS_BIN_DIR` (PATH), `GOMEMLIMIT` (64MiB in the image).

## Budgets

Idle RSS < 40 MB, peak < 150 MB while processing a photo (vips child
included). Originals are stored byte for byte (sha256 of stored file =
sha256 of upload). Image: linux/amd64 + linux/arm64.

## Git

Commit only at phase gates or when asked, with clear messages. Don't create
the GitHub repo or push without asking the owner.
