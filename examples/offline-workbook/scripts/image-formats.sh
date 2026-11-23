set -eo pipefail
for ext in gif tiff bmp; do magick /source/harbor.png -resize 240 "map.$ext"; identify "map.$ext"; done
