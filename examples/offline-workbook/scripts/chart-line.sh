set -eo pipefail
chart line --x hour --y Pier,Museum,North --title 'Harborside departures' /source/day.csv --out line.png
identify line.png
