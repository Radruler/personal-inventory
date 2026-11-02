#!/usr/bin/env bash
# Measure the memory budgets of PLAN.md §2 on the production image:
# idle RSS < 40 MB and peak < 150 MB while a photo is processed (the server
# process plus its vips children).
#
#   scripts/measure-memory.sh
#
# Environment:
#   IMAGE     image to test (default inv-mem:local, built from this repo)
#   NO_BUILD  1 = use IMAGE as is (e.g. IMAGE=ghcr.io/radruler/personal-inventory:latest)
#   NAME      container name (default inv-mem); volumes NAME-data, NAME-media
#   PORT      host port on 127.0.0.1 (default 8496)
#   FIXTURES  directory with IMG_4801.HEIC / IMG_4802.JPG (generated if missing)
#   KEEP      1 = keep the container and volumes afterwards
#
# What is measured, and which number the budget means:
#   - VmRSS / VmHWM of PID 1 (inv serve) from /proc/1/status: resident
#     memory of the Go process now / at its high-water mark.
#   - While a photo is processed, a sampler inside the container reads
#     /proc/*/status every ~20 ms and sums VmRSS of `inv` and every `vips*`
#     child: "app + vips RSS". THIS is the budget number (< 150 MB peak,
#     < 40 MB idle, where idle has no children).
#   - cgroup v2 memory.current / memory.peak / memory.stat (anon, file): the
#     container's charge. It includes page cache (file) for the photo bytes
#     written and read, which the kernel reclaims under pressure, so it
#     overstates the process footprint. `anon` is close to the processes'
#     private memory; memory.peak is an upper bound. `docker stats` shows
#     memory.current minus inactive file.
#   memory.peak is reset by restarting the container (the cgroup is
#   recreated); the sysfs file is read-only inside an unprivileged container.
#
# The fixtures mirror internal/media's TestMemory48MPHEIC: an iPhone-shaped
# 48 MP HEIC (8064x6048 as a 16x12 grid of 512 px HEVC tiles, three
# distinct gradient tiles reused) built with libheif's heif-enc, and a 48 MP
# JPEG (8064x6048). They are generated in a throwaway alpine:3.22 container.
# Single-quoted strings are shell run inside the container; results are
# assigned with printf -v.
# shellcheck disable=SC2016,SC2013,SC2154
set -euo pipefail

cd "$(dirname "$0")/.."
IMAGE=${IMAGE:-inv-mem:local}
NAME=${NAME:-inv-mem}
PORT=${PORT:-8496}
URL=http://127.0.0.1:$PORT
work=$(mktemp -d)
FIXTURES=${FIXTURES:-$work/fixtures}

cleanup() {
	if [[ -n ${sampler:-} ]]; then kill "$sampler" 2>/dev/null || true; fi
	if [[ ${KEEP:-0} != 1 ]]; then
		docker rm -f "$NAME" >/dev/null 2>&1 || true
		docker volume rm "$NAME-data" "$NAME-media" >/dev/null 2>&1 || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT

if [[ ${NO_BUILD:-0} != 1 ]]; then
	echo "building $IMAGE…"
	docker build -q -t "$IMAGE" --build-arg VERSION=measure . >/dev/null
fi

if [[ ! -f $FIXTURES/IMG_4801.HEIC || ! -f $FIXTURES/IMG_4802.JPG ]]; then
	echo "generating 48 MP fixtures…"
	mkdir -p "$FIXTURES"
	docker run --rm -v "$FIXTURES:/out" alpine:3.22 sh -euc '
		apk add -q --no-cache libheif-tools vips-tools vips-heif >/dev/null
		cd /out; mkdir -p tiles
		for s in 20 80 140; do
			awk -v s=$s "BEGIN { w = 512; h = 512; print \"P3\n\" w \" \" h \"\n255\"
				for (y = 0; y < h; y++) { l = \"\"; for (x = 0; x < w; x++) l = l xor(int(x*255/w), s) \" \" int(y*255/h) \" \" s \" \"; print l } }" >tiles/g$s.ppm
			vips copy tiles/g$s.ppm tiles/g$s.png
		done
		i=0
		for y in $(seq -w 0 11); do for x in $(seq -w 0 15); do
			case $((i % 3)) in 0) s=20 ;; 1) s=80 ;; *) s=140 ;; esac
			cp tiles/g$s.png tiles/tile-$y-$x.png; i=$((i + 1))
		done; done
		heif-enc -q 50 -T --tiled-image-width 8064 --tiled-image-height 6048 -o IMG_4801.HEIC tiles/tile-00-00.png >/dev/null
		vips replicate tiles/g80.png big.v 16 12
		vips crop big.v "IMG_4802.JPG[Q=85]" 0 0 8064 6048
		rm -rf big.v tiles'
fi
for f in IMG_4801.HEIC IMG_4802.JPG; do
	echo "fixture $f: $(wc -c <"$FIXTURES/$f" | tr -d ' ') bytes"
done

dx() { docker exec "$NAME" "$@"; }
wait_health() {
	for _ in $(seq 1 100); do
		curl -fsS "$URL/api/health" >/dev/null 2>&1 && return 0
		sleep 0.1
	done
	echo "server did not come up"; docker logs "$NAME"; exit 1
}
mb() { awk -v k="$1" 'BEGIN { printf "%.1f", k / 1024 }'; }
proc1() { dx sh -c 'grep -E "^(VmRSS|VmHWM|RssAnon|RssFile):" /proc/1/status' | awk '{ printf "%s %.1f MB  ", $1, $2 / 1024 }'; }
cgroup() {
	dx sh -c 'c=$(cat /sys/fs/cgroup/memory.current); p=$(cat /sys/fs/cgroup/memory.peak 2>/dev/null || echo 0)
		awk -v c=$c -v p=$p "/^(anon|file) / { v[\$1] = \$2 } END { printf \"cgroup memory.current %.1f MB  memory.peak %.1f MB  anon %.1f MB  file %.1f MB\", c/1048576, p/1048576, v[\"anon\"]/1048576, v[\"file\"]/1048576 }" /sys/fs/cgroup/memory.stat'
}
restart() { docker restart -t 10 "$NAME" >/dev/null; wait_health; }

docker rm -f "$NAME" >/dev/null 2>&1 || true
docker volume rm "$NAME-data" "$NAME-media" >/dev/null 2>&1 || true
docker run -d --name "$NAME" -p "127.0.0.1:$PORT:8480" \
	-v "$NAME-data:/data" -v "$NAME-media:/media" \
	--log-driver local --log-opt max-size=1m "$IMAGE" >/dev/null
wait_health
echo "health: $(curl -fsS "$URL/api/health")"
echo "seeding demo data…"
dx inv seed --demo >/dev/null

restart
sleep 10
printf -v "idle_cold" %s "$(proc1)"
printf -v "idle_cold_cg" %s "$(cgroup)"

# Warm idle: after a browse round (list, facets, locations, items, thumbs).
items=$(curl -fsS "$URL/api/items?limit=200&lifecycle=all")
for p in /api/schema /api/facets /api/locations /api/staging /api/inbox / ; do curl -fsS -o /dev/null "$URL$p"; done
for id in $(grep -oE '"id":"[0-9A-Z]{26}","kind"' <<<"$items" | cut -d'"' -f4); do curl -fsS -o /dev/null "$URL/api/items/$id"; done
for t in $(grep -oE '/media/[0-9a-f]{64}/thumb' <<<"$items" | sort -u); do curl -fsS -o /dev/null "$URL$t"; done
sleep 10
printf -v "idle_warm" %s "$(proc1)"
printf -v "idle_warm_cg" %s "$(cgroup)"

# The sampler: one line per ~20 ms: inv_rss_kb max_vips_rss_kb inv+vips_kb nprocs cgroup_anon_kb cgroup_current_kb.
# Only builtins in the loop (plus sleep), so it barely perturbs what it measures.
sampler_sh='
echo $$ >/tmp/sampler.pid
while :; do
	inv=0 vmax=0 vsum=0 np=0
	for d in /proc/[0-9]*; do
		n="" r=0
		{ while read -r k v _; do case $k in Name:) n=$v ;; VmRSS:) r=$v; break ;; esac; done <"$d/status"; } 2>/dev/null
		case $n in
		inv) inv=$r ;;
		vips*) vsum=$((vsum + r)); np=$((np + 1)); [ "$r" -gt "$vmax" ] && vmax=$r ;;
		esac
	done
	anon=0
	while read -r k v; do [ "$k" = anon ] && { anon=$((v / 1024)); break; }; done </sys/fs/cgroup/memory.stat
	read -r cur </sys/fs/cgroup/memory.current
	echo "$inv $vmax $((inv + vsum)) $np $anon $((cur / 1024))"
	sleep 0.02
done'

measure_upload() { # key file
	local key=$1 file=$2 id st samples=$work/$1.samples
	restart
	sleep 3
	printf -v "${key}_before" %s "$(cgroup)"
	docker exec "$NAME" sh -c "$sampler_sh" >"$samples" 2>/dev/null &
	sampler=$!
	sleep 0.5
	local t0=$SECONDS
	id=$(curl -fsS -F "file=@$FIXTURES/$file" "$URL/api/media" | grep -oE '"id":"[0-9A-Z]{26}"' | head -1 | cut -d'"' -f4)
	for _ in $(seq 1 600); do
		st=$(curl -fsS "$URL/api/media/$id" | grep -oE '"variant_status":"[a-z]+"' | cut -d'"' -f4)
		[[ $st == pending ]] || break
		sleep 0.2
	done
	sleep 0.5
	dx sh -c 'kill "$(cat /tmp/sampler.pid)"; rm -f /tmp/sampler.pid' 2>/dev/null || true
	wait "$sampler" 2>/dev/null || true
	sampler=""
	printf -v "${key}_status" %s "variants $st in $((SECONDS - t0)) s; $(wc -l <"$samples" | tr -d ' ') samples"
	local peak
	peak=$(awk 'NF == 6 { if ($1 > a) a = $1; if ($2 > b) b = $2; if ($3 > c) c = $3; if ($5 > e) e = $5; if ($6 > f) f = $6; if ($4 > n) n = $4 }
		END { printf "app+vips RSS peak %.1f MB (inv %.1f MB, largest vips child %.1f MB, max %d vips procs at once)  cgroup anon peak %.1f MB  memory.current peak %.1f MB", c/1024, a/1024, b/1024, n, e/1024, f/1024 }' "$samples")
	printf -v "${key}_peak" %s "$peak"
	printf -v "${key}_after" %s "$(proc1)| $(cgroup)"
	[[ $st == ready ]] || { echo "$file: variants $st"; docker logs --tail 20 "$NAME"; }
}
measure_upload heic IMG_4801.HEIC
measure_upload jpeg IMG_4802.JPG

cat <<EOF

== $IMAGE ($(curl -fsS "$URL/api/health" | grep -oE '"vips":"[^"]+"'))
Idle after restart (10 s):    ${idle_cold}
                              ${idle_cold_cg}
Idle after browsing (10 s):   ${idle_warm}
                              ${idle_warm_cg}
48 MP HEIC (8064x6048 grid):  ${heic_status}
                              before: ${heic_before}
                              ${heic_peak}
                              after:  ${heic_after}
48 MP JPEG (8064x6048):       ${jpeg_status}
                              before: ${jpeg_before}
                              ${jpeg_peak}
                              after:  ${jpeg_after}
Budgets: idle VmRSS < 40 MB; app+vips RSS peak < 150 MB.
EOF
