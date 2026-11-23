set -eo pipefail
cat /source/day.csv | chart line --x hour --y Pier > departures.svg
head -c 165 departures.svg
