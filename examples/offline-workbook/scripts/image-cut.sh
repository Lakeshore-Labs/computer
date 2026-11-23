set -eo pipefail
magick /source/harbor.png -crop 440x230+175+65 -rotate 90 section-turn.png
identify section-turn.png
