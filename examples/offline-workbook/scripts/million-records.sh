set -eo pipefail
sqlite3 million.db "
DROP TABLE IF EXISTS trips;
CREATE TABLE trips(route TEXT NOT NULL, riders INTEGER NOT NULL);
WITH RECURSIVE seq(n) AS (VALUES(0) UNION ALL SELECT n+1 FROM seq WHERE n<999999)
INSERT INTO trips SELECT CASE n%3 WHEN 0 THEN 'Pier' WHEN 1 THEN 'Museum' ELSE 'North' END, 5 + (n*37)%79 FROM seq;
"
sqlite3 -header -csv million.db 'SELECT route,COUNT(*) AS trips,SUM(riders) AS boardings FROM trips GROUP BY route ORDER BY boardings DESC' > million-summary.csv
cat million-summary.csv
printf 'Database bytes: '; wc -c < million.db
printf 'Summary bytes: '; wc -c < million-summary.csv
chart bar --x route --y boardings --title 'One million synthetic ferry trips' million-summary.csv --out million.png
identify million.png
