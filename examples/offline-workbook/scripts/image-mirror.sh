set -eo pipefail
magick /source/harbor.png -flip -flop -resize 50% mirrored.png
identify mirrored.png
