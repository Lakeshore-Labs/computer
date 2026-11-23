set -eo pipefail
xan agg 'sum(Pier) as Pier, sum(Museum) as Museum, sum(North) as North' /source/day.csv
