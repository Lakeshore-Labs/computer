set -eo pipefail
chart scatter --x hour --y Pier,Museum,North --title 'Harborside departures' /source/day.csv --out scatter.png
identify scatter.png
