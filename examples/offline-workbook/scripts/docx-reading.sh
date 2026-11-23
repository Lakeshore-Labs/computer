set -eo pipefail
python3 - <<'PY'
import mammoth
from pathlib import Path

with open("/source/brief.docx", "rb") as f:
    html = mammoth.convert_to_html(f).value
Path("word-brief.html").write_text(html)
print(html)
PY
html-to-markdown word-brief.html
