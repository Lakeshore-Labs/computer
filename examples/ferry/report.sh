set -eo pipefail
# The filesystem server returns the text under its structured "content" field.
payload=$(files read-text-file --path boardings.json | jq -r '.content' | sed "s/'/''/g")

sqlite3 -header -csv :memory: "
  SELECT value->>'route' AS route, SUM(value->>'riders') AS boardings
  FROM json_each('$payload')
  GROUP BY route ORDER BY boardings DESC
" > totals.csv

cat totals.csv
chart bar --x route --y boardings --title 'Harborside · passenger boardings' totals.csv --out ferry.png
identify ferry.png
