set -eo pipefail
awk -F, '
  BEGIN { print "distribution,registrations" }
  NR > 1 { remote += $2; package_only += $3; both += $4; neither += $5 }
  END {
    printf "Remote only,%d\nPackage only,%d\nBoth,%d\nNeither declared,%d\n", remote, package_only, both, neither
  }
' page-counts.csv > distribution.csv
chart bar --x distribution --y registrations distribution.csv --out distribution.png
cat distribution.csv
