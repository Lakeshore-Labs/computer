set -eo pipefail
html-to-markdown /source/notice.html > notice.md
cat notice.md
