set -eo pipefail
# Computer runs this script. Each section prints a header naming the MCP server, the command it
# became in Computer, and the tool called, so that render.py can show which server made which output.
# run.py replaces @REPO@ with the absolute path of the fixture repository; the Git server runs on
# the host, so it needs the host's path, not a guest path.
section() { printf '\n=== %s server · command: %s · tool: %s ===\n' "$1" "$2" "$3"; }

section Filesystem files read-text-file
files read-text-file --path boardings.json | jq -r '.content' | jq 'length'

section Git historybook git-log
historybook git-log --repo-path @REPO@ --max-count 1 | sed -n '/^Message:/p'

section Time clock convert-time
# The tool uses today's date, so only the time of day is printed; the transcript then stays the same on any day.
clock convert-time --source-timezone UTC --time 08:00 --target-timezone Asia/Tokyo | jq -r '.target | [.timezone, (.datetime | sub("^[^T]*T"; ""))] | @tsv'

section Memory memory 'create-entities, search-nodes'
memory create-entities --json '{"entities":[{"name":"Pier","entityType":"route","observations":["310 boardings"]}]}' > /tmp/created.json
memory search-nodes --query Pier | jq -r '.entities[] | .name + ": " + (.observations | join(", "))'

section 'Sequential Thinking' steps sequentialthinking
steps sequentialthinking --json '{"thought":"Count each route.","thoughtNumber":1,"totalThoughts":2,"nextThoughtNeeded":true}' > /tmp/first.json
steps sequentialthinking --json '{"thought":"Check the totals.","thoughtNumber":2,"totalThoughts":2,"nextThoughtNeeded":false}' | jq '{thoughtHistoryLength,nextThoughtNeeded}'

section Fetch webpage fetch
webpage fetch --url https://example.com/ --max-length 600 | sed -n '/^# /p'

section Everything everything get-tiny-image
img=$(everything get-tiny-image | tail -1)
cp "$img" logo.png
identify logo.png
