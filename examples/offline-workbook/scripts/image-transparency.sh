set -eo pipefail
magick /source/boat.png -background '#b0d6d5' -alpha remove -resize '300x200!' boat-card.jpg
identify boat-card.jpg
