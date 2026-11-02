#!/usr/bin/env bash
# Zero-write soak test against a running container (PLAN.md §2): no disk
# writes while idle and while browsing read-only. Works against the NAS
# deployment as well as a local container.
#
#   scripts/soak.sh [-c CONTAINER] [-u URL] [-i IDLE_SECONDS] [-m BROWSE_MINUTES]
#
# Defaults: -c personal-inventory -u http://localhost:8480 -i 60 -m 5.
# Set DOCKER="sudo docker" if your docker needs sudo. If INV_TOKEN is set it
# is sent as a bearer token (reads don't need it, but it does no harm).
#
# What it checks, for the idle window and then the browse window:
#   1. /proc/1/io write_bytes of the server (PID 1 in the container; includes
#      vips children it has reaped) does not grow.
#   2. No file under /data or /media is created, removed, resized, or has its
#      mtime changed (a `find … -newer` against a marker plus a full
#      name/size/mtime listing), including inv.db, inv.db-wal and inv.db-shm.
#
# Notes:
#   - The marker lives in the container's /tmp (writable layer), not in the
#     measured volumes, and is removed at the end.
#   - write_bytes is only meaningful on filesystems that do page-cache
#     accounting (ext4, xfs, overlayfs on those, ZFS on Linux). The
#     name/size/mtime check is filesystem independent.
#   - The SQLite -shm file is the WAL index. Right after a write the first
#     reader may record one read mark in it (one 4 KiB page); browsing after
#     that leaves it untouched. Run the soak a minute after the last write.
#   - The browse loop only sends GET/HEAD requests.
# Single-quoted strings are shell run inside the container.
# shellcheck disable=SC2016
set -euo pipefail

CONTAINER=personal-inventory
URL=http://localhost:8480
IDLE=60
MINUTES=5
while getopts "c:u:i:m:h" opt; do
	case $opt in
	c) CONTAINER=$OPTARG ;;
	u) URL=${OPTARG%/} ;;
	i) IDLE=$OPTARG ;;
	m) MINUTES=$OPTARG ;;
	*) sed -n '2,30p' "$0"; exit 2 ;;
	esac
done
DOCKER=${DOCKER:-docker}
CURL=(curl -fsS --max-time 30)
[[ -n ${INV_TOKEN:-} ]] && CURL+=(-H "Authorization: Bearer $INV_TOKEN")

work=$(mktemp -d)
trap 'rm -rf "$work"; $DOCKER exec "$CONTAINER" rm -f /tmp/soak-mark >/dev/null 2>&1 || true' EXIT

dx() { $DOCKER exec "$CONTAINER" "$@"; }
write_bytes() { dx sh -c 'sed -n "s/^write_bytes: //p" /proc/1/io'; }
listing() { dx sh -c 'find /data /media -type f -exec stat -c "%n %s %Y" {} + 2>/dev/null | sort'; }
dbfiles() { dx sh -c 'for f in /data/inv.db /data/inv.db-wal /data/inv.db-shm; do [ -e "$f" ] && stat -c "%n %s B mtime %y" "$f" || echo "$f absent"; done'; }
rss() { dx sh -c 'grep -E "^(VmRSS|VmHWM)" /proc/1/status | tr -s " \t" " " | paste -sd" " -'; }

fail=0
check() { # phase w_before list_before
	local phase=$1 w0=$2 l0=$3 w1 newer pfail=0
	w1=$(write_bytes)
	listing >"$work/$phase.after"
	newer=$(dx sh -c 'find /data /media -newer /tmp/soak-mark 2>/dev/null' || true)
	echo "[$phase] write_bytes delta: $((w1 - w0))"
	if [[ $w1 != "$w0" ]]; then
		echo "[$phase] FAIL: write_bytes grew by $((w1 - w0))"
		pfail=1
	fi
	if [[ -n $newer ]]; then
		echo "[$phase] FAIL: paths modified after the marker:"
		while read -r p; do echo "    $p"; done <<<"$newer"
		pfail=1
	fi
	if ! diff -u "$l0" "$work/$phase.after" >"$work/$phase.diff"; then
		echo "[$phase] FAIL: file listing changed:"
		sed 's/^/    /' "$work/$phase.diff" | head -40
		pfail=1
	fi
	if [[ $pfail == 0 ]]; then echo "[$phase] OK: no writes"; else fail=1; fi
	dbfiles | sed "s/^/[$phase]   /"
	return 0
}

echo "container $CONTAINER, $URL; idle ${IDLE}s, browse ${MINUTES}m"
"${CURL[@]}" "$URL/api/health" >/dev/null || { echo "server not reachable at $URL"; exit 1; }
"${CURL[@]}" "$URL/api/health"; echo
echo "server memory: $(rss)"

# Collect browse targets before measuring (these are reads too).
items=$("${CURL[@]}" "$URL/api/items?limit=200&lifecycle=all")
ids=$(grep -oE '"id":"[0-9A-HJKMNP-TV-Z]{26}","kind"' <<<"$items" | cut -d'"' -f4 | sort -u)
thumbs=$(grep -oE '/media/[0-9a-f]{64}/thumb' <<<"$items" | sort -u)
shas=$(sed -E 's#/media/([0-9a-f]{64})/thumb#\1#' <<<"$thumbs" | head -8)
locs=$("${CURL[@]}" "$URL/api/locations")
locids=$(grep -oE '"id":"[0-9A-HJKMNP-TV-Z]{26}","code"' <<<"$locs" | cut -d'"' -f4 | sort -u)
codes=$(grep -oE '"code":"[^"]+"' <<<"$locs" | cut -d'"' -f4 | sort -u)
asset=$("${CURL[@]}" "$URL/" | grep -oE '/_app/immutable/[^"'"'"' ]+\.(js|css)' | head -1 || true)
echo "browse targets: $(wc -w <<<"$ids") items, $(wc -w <<<"$locids") locations, $(wc -w <<<"$codes") codes, $(wc -w <<<"$thumbs") thumbs${asset:+, asset $asset}"

urls=("/api/health" "/api/schema" "/api/items" "/api/items?q=uniqlo" "/api/items?q=jack&sort=relevance"
	"/api/items?kind=clothing&sort=title" "/api/items?status=staged" "/api/items?status=unknown"
	"/api/items?review=needs_review" "/api/items?lifecycle=all&sort=created&limit=12" "/api/items?attr.color=navy"
	"/api/facets" "/api/facets?kind=clothing" "/api/identifiers/lookup?value=12345" "/api/products"
	"/api/locations" "/api/staging" "/api/inbox" "/" "/items" "/locations")
for id in $ids; do urls+=("/api/items/$id"); done
for id in $locids; do urls+=("/api/locations/$id"); done
for c in $codes; do urls+=("/api/locations/code:$c"); done
for t in $thumbs; do urls+=("$t"); done
for s in $shas; do urls+=("/media/$s/display" "/media/$s/orig"); done
[[ -n $asset ]] && urls+=("$asset")
printf '%s\n' "${urls[@]/#/$URL}" >"$work/urls"
# curl config: every URL's body goes to /dev/null.
awk '{ print "url = \"" $0 "\""; print "output = \"/dev/null\"" }' "$work/urls" >"$work/curlrc"

# Idle window.
dx touch /tmp/soak-mark
sleep 1.1 # mtime granularity guard for -newer
listing >"$work/base"
w0=$(write_bytes)
echo "idling ${IDLE}s…"
sleep "$IDLE"
check idle "$w0" "$work/base"

# Browse window: loop the URL list until the time is up.
w0=$(write_bytes)
end=$(($(date +%s) + MINUTES * 60))
rounds=0 reqs=0 errs=0
echo "browsing for ${MINUTES}m ($(wc -l <"$work/urls" | tr -d ' ') requests per round)…"
while (($(date +%s) < end)); do
	# One curl process per round; keep-alive across the round like a browser.
	out=$(curl -sS --max-time 120 ${INV_TOKEN:+-H "Authorization: Bearer $INV_TOKEN"} \
		-w '%{http_code}\n' --config "$work/curlrc" || true)
	n=$(wc -l <<<"$out" | tr -d ' ')
	bad=$(grep -vcE '^(200|206|304)$' <<<"$out" || true)
	reqs=$((reqs + n)) errs=$((errs + bad)) rounds=$((rounds + 1))
	"${CURL[@]}" -o /dev/null -I "$URL/api/health" || errs=$((errs + 1)) # a HEAD too
	sleep 0.5
done
echo "browse: $rounds rounds, $reqs requests, $errs non-2xx/304"
((errs == 0)) || { echo "[browse] FAIL: $errs requests failed"; fail=1; }
check browse "$w0" "$work/base"
echo "server memory: $(rss)"

if [[ $fail == 0 ]]; then echo "PASS: zero writes idle and while browsing"; else echo "FAIL"; fi
exit $fail
