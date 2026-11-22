set -eo pipefail

printf '\n[Filesystem] Count the departures served by the tool.\n'
files read-text-file --path boardings.json | jq -r '.content' | jq 'length'

printf '\n[Git] Read the synthetic fixture commit.\n'
historybook git-log --repo-path . --max-count 1 | sed -n '/^Message:/p'

printf '\n[Time] Convert 08:00 UTC to Tokyo time.\n'
clock convert-time --source-timezone UTC --time 08:00 --target-timezone Asia/Tokyo | jq -r '.target | [.timezone,.datetime] | @tsv'

printf '\n[Memory] Store a route observation and read it back.\n'
memory create-entities --json '{"entities":[{"name":"Pier","entityType":"route","observations":["310 boardings"]}]}' > /tmp/created.json
memory search-nodes --query Pier | jq -r '.entities[] | .name + ": " + (.observations | join(", "))'

printf '\n[Sequential Thinking] Record two planning milestones.\n'
steps sequentialthinking --json '{"thought":"Count each route.","thoughtNumber":1,"totalThoughts":2,"nextThoughtNeeded":true}' > /tmp/first.json
steps sequentialthinking --json '{"thought":"Check the totals.","thoughtNumber":2,"totalThoughts":2,"nextThoughtNeeded":false}' | jq '{thoughtHistoryLength,nextThoughtNeeded}'

printf '\n[Fetch] Read the title of the public example page.\n'
webpage fetch --url https://example.com/ --max-length 600 | sed -n '/^# /p'

printf '\n[Everything] Save the image content returned by the tool.\n'
img=$(everything get-tiny-image | tail -1)
cp "$img" logo.png
identify logo.png
