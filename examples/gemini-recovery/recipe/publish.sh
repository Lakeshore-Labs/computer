set -eu
{
  cat /recipe/desk.html
  printf '%s\n' '<script type="application/json" id="receipt">'
  jq -c . /capture/checks.json | sed 's/</\\u003c/g'
  printf '%s\n' '</script><script>'
  cat /recipe/desk.js
  printf '%s\n' '</script></html>'
} > claim.html
