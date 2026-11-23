set -eo pipefail
jq -r '.[] | select(.riders >= 45) | [.slot,.route,.riders] | @tsv' /source/boardings.json | sort
