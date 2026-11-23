set -eo pipefail
python3 - <<'PY'
from pathlib import Path
import yaml
from jinja2 import Template
from bs4 import BeautifulSoup

data = yaml.safe_load(Path("/source/routes.yaml").read_text())
template = Template(
    "<article><h1>{{ service }}</h1>{% for r in routes %}<p>{{ r.name }} · capacity {{ r.capacity }}</p>{% endfor %}</article>",
    autoescape=True,
)
html = template.render(**data)
Path("service-notice.html").write_text(html)
for line in BeautifulSoup(html, "html.parser").stripped_strings:
    print(line)
PY
