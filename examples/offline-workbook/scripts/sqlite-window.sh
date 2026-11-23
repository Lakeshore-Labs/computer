set -eo pipefail
sqlite3 -header -table :memory: "WITH trips(route,n) AS (VALUES ('Museum',225),('Pier',310),('North',184)) SELECT rank() OVER (ORDER BY n DESC) AS rank,route,n FROM trips ORDER BY rank"
