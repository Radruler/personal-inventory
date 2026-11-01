#!/bin/sh
# Regenerates the committed HEIC fixtures. Needs vips, exiftool and heif-enc
# (apk add libheif-tools), so run it in a throwaway dev container:
#
#   docker compose -f .devcontainer/docker-compose.yml run --rm -T dev \
#     sh -c 'apk add --no-cache libheif-tools && sh internal/media/testdata/make-fixtures.sh'
#
# IMG_0001.HEIC  320x240 coded, irot = 90° clockwise (EXIF Orientation 6 as an
#                iPhone writes it), EXIF DateTimeOriginal / OffsetTimeOriginal /
#                SubSecTimeOriginal, Make/Model/Lens/Software and GPS tags (GPS
#                must never reach media.exif or the variants). A red square
#                marks the coded top-left corner.
# grid.heic      a 3x2 grid of 256px tiles written by libheif's own encoder
#                (heif-enc -T), cropped to 700x480, Display P3 ICC profile.
#                Each tile is a different solid colour.
set -eu
out=$(cd "$(dirname "$0")" && pwd)
w=$(mktemp -d)
trap 'rm -rf "$w"' EXIT
cd "$w"

# --- IMG_0001.HEIC
vips xyz xy.v 320 240
vips extract_band xy.v x.v 0
vips extract_band xy.v y.v 1
vips linear x.v r.v 0.5 40 --uchar
vips linear y.v g.v 0.8 30 --uchar
vips linear x.v b.v --uchar -- -0.3 200
vips bandjoin "r.v g.v b.v" rgb.v
vips copy rgb.v srgb.v --interpretation srgb
vips black sq.v 60 60 --bands 3
vips linear sq.v red.v "1 1 1" "230 20 20" --uchar
vips insert srgb.v red.v marked.v 0 0
vips copy marked.v img.jpg[Q=90]
exiftool -q -overwrite_original \
  -Make=Apple -Model="iPhone 15 Pro" -LensMake=Apple \
  -LensModel="iPhone 15 Pro back triple camera 6.765mm f/1.78" \
  -Software=18.1 -HostComputer="iPhone 15 Pro" \
  -DateTimeOriginal="2026:10:30 14:05:09" -SubSecTimeOriginal=123 \
  -OffsetTimeOriginal="-07:00" -OffsetTime="-07:00" \
  -ExposureTime=0.008333 -FNumber=1.78 -ISO=80 -FocalLength=6.765 -FocalLengthIn35mmFormat=24 \
  -Orientation#=6 \
  -GPSLatitude=37.334606 -GPSLatitudeRef=N -GPSLongitude=122.009102 -GPSLongitudeRef=W \
  -GPSAltitude=12.5 -GPSAltitudeRef=0 \
  img.jpg
# vips carries EXIF over and libheif writes Orientation 6 as an irot box.
vips copy img.jpg "$out/IMG_0001.HEIC[Q=60]"

# --- grid.heic
i=0
for y in 00 01; do
  for x in 00 01 02; do
    set -- "200 40 40" "40 160 60" "40 70 200" "220 200 40" "160 50 170" "40 190 190"
    shift $i
    vips black t.v 256 256 --bands 3
    vips linear t.v c.v "1 1 1" "$1" --uchar
    vips copy c.v cs.v --interpretation srgb
    vips icc_transform cs.v "tile-$y-$x.png" p3 --input-profile srgb
    i=$((i + 1))
  done
done
heif-enc -q 60 -T tile-00-00.png --tiled-image-width 700 --tiled-image-height 480 -o "$out/grid.heic"

ls -l "$out"/*.HEIC "$out"/*.heic
