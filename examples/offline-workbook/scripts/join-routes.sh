set -eo pipefail
jq -r 'group_by(.route)[] | [.[0].route, (map(.riders)|add)] | @tsv' /source/boardings.json | sort > counts.tsv
yq -o json '.' /source/routes.yaml | jq -r '.routes[] | [.name,.capacity] | @tsv' | sort > capacity.tsv
join counts.tsv capacity.tsv
