set -eo pipefail
chart bar --x hour --y Pier,Museum,North --title 'Harborside departures' /source/day.csv --out bar.png
identify bar.png
