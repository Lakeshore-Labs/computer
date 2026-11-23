set -eo pipefail
python3 - <<'PY'
import tomllib, tomli_w
from pathlib import Path

p = Path("service.toml")
p.write_text(tomli_w.dumps({"ferry": {"routes": 3, "departures": 24}}))
print(p.read_text(), end="")
assert tomllib.loads(p.read_text())["ferry"]["departures"] == 24
PY
