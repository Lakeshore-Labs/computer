"""Run inside Computer's WASI Python and embed the observed JPEGs in one offline viewer."""
import base64
from datetime import date
import hashlib
import json
from pathlib import Path
import sys

source = Path(sys.argv[1])
pinned = json.loads(Path('/scenes/receipts.json').read_text())
scenes = []
for record in pinned:
    day = date.fromisoformat(record['date'])
    data = (source / (day.isoformat() + '.jpg')).read_bytes()
    if not data.startswith(b'\xff\xd8\xff') or not data.endswith(b'\xff\xd9'):
        raise ValueError(f'{day} did not return a JPEG')
    digest = hashlib.sha256(data).hexdigest()
    if source == Path('/scenes') and digest != record['sha256']:
        raise ValueError(f'The saved JPEG for {day} differs from its receipt')
    scenes.append({'date': day.isoformat(), 'label': day.strftime('%b %d'), 'bytes': len(data),
                   'sha256': digest, 'image': 'data:image/jpeg;base64,' + base64.b64encode(data).decode()})

if len(scenes) != 4 or len({s['sha256'] for s in scenes}) != 4:
    raise ValueError('The viewer needs four distinct scenes')
template = Path('/recipe/desk.html').read_text()
if template.count('__SCENES__') != 1:
    raise ValueError('The viewer template is missing the scene data slot')
Path('desk.html').write_text(template.replace('__SCENES__', json.dumps(scenes, separators=(',', ':'))))
receipts = [{k: v for k, v in scene.items() if k != 'image'} for scene in scenes]
Path('receipts.json').write_text(json.dumps(receipts, indent=2) + '\n')
print(f'{len(scenes)} dated scenes, {sum(s["bytes"] for s in scenes):,} source bytes')
