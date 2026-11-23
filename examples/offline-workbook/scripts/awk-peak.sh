set -eo pipefail
awk -F, 'NR>1 {n=$2+$3+$4; if(n>peak){peak=n;slot=$1}} END {print slot,peak,"riders"}' /source/day.csv
