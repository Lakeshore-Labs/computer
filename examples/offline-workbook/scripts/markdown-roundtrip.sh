set -eo pipefail
python3 - <<'PY'
import markdown
from markdownify import markdownify
from pathlib import Path

source = "# Last departure\n\nLeaves the **Pier** at 20:00."
html = markdown.markdown(source)
Path("last-departure.html").write_text(html)
print(markdownify(html, heading_style="ATX").strip())
PY
