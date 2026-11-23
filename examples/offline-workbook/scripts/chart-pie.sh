set -eo pipefail
chart pie --x hour --y Pier --title 'Harborside departures' /source/day.csv --out pie.png
identify pie.png
