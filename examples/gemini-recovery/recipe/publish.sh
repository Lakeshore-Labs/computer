set -euo pipefail
stage=$(mktemp ./claim.XXXXXX)
trap 'rm -f "$stage"' EXIT
{
  cat /recipe/desk.html
  printf '%s\n' '<script type="application/json" id="receipt">'
  jq -cse 'select(length == 1) | .[0] | select(
    .passed == true and
    [.events[].step] == ["opened","packing","interrupted","reopened","finished"] and
    all(.events[]; (.elapsed_s | type) == "number" and
      ((.terminal_events // []) | type == "array" and all(.[]; type == "string"))) and
    (.events[0].child_process | type) == "number" and (.events[1].in_progress | type) == "string" and
    (.events[1].replay_markers | type) == "number" and (.events[2].exit | type) == "number" and
    (.events[2].terminal_events | type) == "array" and (.events[3].child_process | type) == "number" and
    (.events[4].notice | type) == "string" and (.events[4].conversations | type) == "number" and
    (.events[4].terminal_events | type) == "array"
  )' /capture/checks.json | sed 's/</\\u003c/g'
  printf '%s\n' '</script><script>'
  cat /recipe/desk.js
  printf '%s\n' '</script></html>'
} > "$stage"
mv "$stage" claim.html
