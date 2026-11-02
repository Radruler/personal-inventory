# Deploying on TrueNAS SCALE

Personal Inventory runs as one container from the public image
`ghcr.io/radruler/personal-inventory`. It has two volumes, `/data` on the SSD
app pool and `/media` on the HDD pool, and listens on port 8480. You reach
it from your phone over Tailscale, and nothing is exposed to the internet.

These steps match the Docker-based app system in TrueNAS SCALE 24.10 and
later (Apps → Discover Apps → Custom App / Install via YAML).

## 1. Datasets

Create two datasets. The paths below are examples, so use your own pool
names and substitute them in the YAML.

| Dataset (example) | Mounted at | Pool | Holds |
|---|---|---|---|
| `ssd/apps/inventory` → `/mnt/ssd/apps/inventory` | `/data` | SSD app pool | `inv.db` (+ `-wal`, `-shm`), `thumbs/`, `backups/` |
| `tank/inventory-media` → `/mnt/tank/inventory-media` | `/media` | HDD pool | `orig/` (originals, byte for byte), `var/` (display variants) |

Thumbnails live on the SSD, so browsing the grid never spins up the HDDs.
The disks spin only when you open a photo at full size.

**Permissions:** the container runs as uid/gid **568**, TrueNAS's `apps`
user. For each dataset, go to *Datasets → (dataset) → Permissions → Edit*,
set the owner user to `apps` and the owner group to `apps`, check *Apply
recursively*, and save. The equivalent from a root shell:

```sh
chown -R 568:568 /mnt/ssd/apps/inventory /mnt/tank/inventory-media
```

A dataset preset of *Apps* (or *Generic*) works. Avoid SMB-share ACL
presets that strip write access from `apps`.

## 2. Install the app

Go to *Apps → Discover Apps*, open the ⋮ menu next to *Custom App*, choose
**Install via YAML**, name the app `personal-inventory`, and paste:

```yaml
services:
  inv:
    image: ghcr.io/radruler/personal-inventory:latest
    container_name: personal-inventory
    restart: unless-stopped
    ports:
      - "8480:8480"
    environment:
      GOMEMLIMIT: 64MiB
      INV_LOG_LEVEL: info
      # Optional: require "Authorization: Bearer <token>" for every write
      # (reads stay open; the web UI asks for the token once).
      # INV_TOKEN: change-me-to-a-long-random-string
    volumes:
      - /mnt/ssd/apps/inventory:/data        # SSD: inv.db, thumbs/, backups/
      - /mnt/tank/inventory-media:/media     # HDD: orig/, var/
    # The default json-file driver writes unbounded logs to disk.
    logging:
      driver: local
      options:
        max-size: 1m
        max-file: "2"
```

Notes:

- There is no Docker `HEALTHCHECK` on purpose, because probe results are
  daemon-persisted state. If you want monitoring, probe
  `http://<nas>:8480/api/health` from outside, for example with Uptime Kuma.
- Leave the access log off (`INV_ACCESS_LOG` unset). It writes a log line
  for every request, which breaks the write budget below. Turn it on only
  while debugging.
- To pin a version, replace `:latest` with a commit tag such as
  `:sha-1a2b3c4` (CI publishes one for every push to `main`) or a release
  tag such as `:1.0.0`.

**The image is public:** the NAS pulls it without logging in. After CI's
first push to GHCR, the owner may need to make the package public once:
*GitHub → Packages → personal-inventory → Package settings → Change
visibility → Public*.

## 3. Reach it from the phone (Tailscale)

1. Install the **Tailscale** app from *Apps → Discover Apps* (or use
   Tailscale on the host if you already run it) and log it in with an auth
   key. The NAS then joins your tailnet under its hostname, for example
   `truenas`.
2. With MagicDNS on (the default), open **`http://truenas:8480`** from any
   device on the tailnet, or use the full name
   `http://truenas.<tailnet>.ts.net:8480`. Install Tailscale on the phone
   and leave it connected at the storage unit.
3. Don't expose it publicly. Don't enable Tailscale **Funnel**, and don't
   forward 8480 on your router. Plain HTTP is fine inside the tailnet
   because WireGuard encrypts the traffic. If you want HTTPS anyway,
   `tailscale serve --bg 8480` on the NAS publishes
   `https://truenas.<tailnet>.ts.net`, which is still reachable only from
   the tailnet.

On the home LAN, the app is also at `http://<nas-ip>:8480`. If anyone else
uses your LAN, set `INV_TOKEN`.

## 4. First run

Open the app. It starts empty, and the first start creates `inv.db` and
runs the migrations. Check `http://truenas:8480/api/health`: it should
report `"status":"ok"` and a `vips` version. `unavailable` means the image
is broken.

**Only to try it out**, fill an empty database with about 60 demo items:

```sh
sudo docker exec personal-inventory inv seed --demo
```

To start over afterwards, stop the app, delete everything inside both
datasets (keep the datasets and their ownership), and start the app again.

## 5. Updating

- **On `:latest`:** TrueNAS shows an update badge on the app when a new
  image digest is available, and the app's *Update* action pulls it and
  recreates the container. From a shell, the equivalent is
  `sudo docker pull ghcr.io/radruler/personal-inventory:latest`, then
  restart the app.
- **Pinned:** *Edit* the app's YAML to the new `:sha-…` or version tag and
  save.

Migrations run automatically at start. Take a backup first (next section).
The data lives only in the two datasets, so recreating the container loses
nothing.

## 6. Backups and restore

**Database copies (`inv backup`).** `inv backup` writes a consistent copy
of the live database with SQLite `VACUUM INTO`. It is safe while the server
runs and never blocks it. The copy is integrity-checked and saved as a
single self-contained file:
`/data/backups/inv-<UTC timestamp>.db`. `--keep N` deletes all but the
newest N timestamped copies in that directory (the default is 7; `0` keeps
everything). It never overwrites an existing file. `inv backup DEST`
writes to another file, or to a directory if DEST is one.

Add a daily job under *System → Advanced Settings → Cron Jobs → Add*:

| Field | Value |
|---|---|
| Description | Personal Inventory DB backup |
| Command | `docker exec personal-inventory inv backup --keep 14` |
| Run As User | `root` |
| Schedule | Daily (e.g. 03:30) |

Each run writes one DB-sized file to the SSD. The demo database is about
440 KB, and a few thousand items is a few MB.

**ZFS snapshots** cover the rest, including the photos. Under *Data
Protection → Periodic Snapshot Tasks*, snapshot **both** datasets daily
with a few weeks' retention. Snapshots are atomic, so a snapshot of `/data`
is crash-consistent for SQLite in WAL mode. The `inv backup` copies are
extra insurance that also survives a deleted dataset once you replicate it
(*Replication Tasks*) or copy it off the box.

**Restore the database:**

1. Stop the app (*Apps → personal-inventory → Stop*).
2. From a root shell:
   ```sh
   cd /mnt/ssd/apps/inventory
   mkdir -p before-restore && mv inv.db inv.db-wal inv.db-shm before-restore/ 2>/dev/null
   cp backups/inv-20261101T033000Z.db inv.db
   chown 568:568 inv.db
   ```
   Always move the old `-wal` and `-shm` away. A leftover WAL from the old
   database would be replayed into the restored one.
3. Start the app and check `/api/health` and a few items.

**Restore photos** by copying files back from a snapshot
(`/mnt/tank/inventory-media/.zfs/snapshot/<snap>/orig/…`), or roll the
dataset back from *Datasets → Snapshots*. If thumbnails are missing (for
example after restoring only `/media`), regenerate every variant with
`sudo docker exec personal-inventory inv media rebuild`.

## 7. Export (plain files, readable without the app)

```sh
sudo docker exec personal-inventory inv export /media/exports/2026-11-01
sudo docker exec personal-inventory inv export --no-media /data/exports/2026-11-01
```

This writes `manifest.json` and one `*.jsonl` per table, plus a copy of
every original under `media/`. `--no-media` skips the originals. A full
export duplicates every original, so write it to the HDD dataset, or use
`--no-media` and rely on snapshots of `/media`. `inv import DIR` restores
an export into an empty database.

## 8. Budgets (measured)

PLAN.md §2 sets the budgets. Measurements from 2026-11-01 on the
production image (Alpine 3.22, vips 8.16.1, `GOMEMLIMIT=64MiB`) with the
demo seed, run with `scripts/measure-memory.sh` (linux/arm64 on Docker
Desktop; the binary and vips are the same code on amd64):

| What | Measured | Budget |
|---|---|---|
| Idle RSS after start (VmRSS of `inv serve`) | **14.8 MB** | < 40 MB |
| Idle RSS after browsing every view | **23.8 MB** | < 40 MB |
| Peak while processing a 48 MP HEIC (8064×6048, 16×12 tile grid) | **90.4 MB** (inv 20.7 + vips child 70.0) | < 150 MB |
| Peak while processing a 48 MP JPEG (8064×6048) | **70.5 MB** (inv 17.9 + vips child 52.6) | < 150 MB |
| Container cgroup `memory.peak` (HEIC / JPEG) | 72.9 MB / 43.5 MB | |

The budget is the RSS of the app process plus its vips children. The
script samples `/proc/*/status` inside the container every ~20 ms and sums
VmRSS of `inv` and `vips*`. cgroup numbers count page cache and shared
pages differently: `memory.peak` was lower here because shared library
pages are counted once, but on a busy box it also includes reclaimable page
cache from writing and reading the photo. `docker stats` shows the cgroup's
usage, which includes page cache, so it reads higher than RSS on a
long-running container. Each 48 MP photo's variants took about 2 s.
Variants are generated one photo at a time, so the peak doesn't grow with
batch uploads.

**Disk writes: zero when idle and while browsing read-only.**
`scripts/soak.sh` checks the server's `/proc/1/io` `write_bytes` and every
file's name, size and mtime under `/data` and `/media`. It found zero
writes after 60 s idle and after 3 minutes and 44,506 read-only requests
(every view, every item and location, all thumbs, display and original
photos, the SPA). The same check runs in CI as an in-process Go test
(`internal/ops` `TestZeroWriteSoak`). As a control, a single edit
registered 60–95 KB of writes, so the detectors work.

What does write:

- An upload writes the original once (HDD), a thumbnail (SSD), a display
  variant (HDD) and a few DB rows. Re-uploading the same photo writes
  nothing.
- Edits, moves and ingest write to the WAL. SQLite checkpoints it into
  `inv.db` every ~1000 pages.
- The `inv backup` cron job writes one file per run. ZFS snapshots write
  nothing until data changes.
- The `-shm` file is SQLite's WAL index (a memory-mapped 32 KB file).
  After a write, the first read may record one read mark in it, and reads
  after that leave it untouched.
- Logs go to the `local` driver, capped at 2×1 MB. At `info` level the
  server logs only at start, at stop and on errors.

Run the soak against the NAS (from a TrueNAS shell, using `sudo` for
docker):

```sh
DOCKER="sudo docker" scripts/soak.sh -c personal-inventory -u http://localhost:8480 -i 120 -m 10
```

Run it a minute after the last write, so the first-reader read mark
described above has already happened. `write_bytes` depends on the
filesystem's page-cache accounting, while the name/size/mtime check works
on any filesystem.

## 9. Troubleshooting

| Symptom | Fix |
|---|---|
| The app won't start, and `docker logs personal-inventory` shows `permission denied` / `unable to open database file` | The dataset isn't owned by 568:568. Fix it (§1) and restart. `ls -ln /mnt/ssd/apps/inventory` should show `568 568`. |
| HEIC photos stay "processing" or show a placeholder | `docker logs personal-inventory` shows `media variants failed` with the reason. Check `/api/health` → `vips`. Re-run with `inv media rebuild`. iOS Safari sometimes uploads JPEG instead of HEIC, which is fine. |
| No logs | `sudo docker logs --tail 100 personal-inventory`. For more detail, set `INV_LOG_LEVEL: debug` (and `INV_ACCESS_LOG: "1"` if needed) temporarily, then remove them, because they write logs for every request. |
| `docker stats` shows more memory than the budget | It includes page cache. Use `sudo docker exec personal-inventory grep VmRSS /proc/1/status` for the process. |
| Upload rejected as too large | Raise `INV_MAX_UPLOAD_MB` (default 64). |
| Port 8480 is taken | Change the left side of `ports:` (e.g. `"8580:8480"`). |
| Phone can't reach it away from home | Tailscale must be connected on the phone. Try the full MagicDNS name or the NAS's `100.x` tailnet IP. |
