"""Build an offline Great Lakes observation desk from saved or live NASA GIBS scenes."""
import argparse
import base64
import hashlib
import json
from pathlib import Path
import re
import shlex
import subprocess
from urllib.parse import urlencode

api = 'https://gibs.earthdata.nasa.gov/wms/epsg4326/best/wms.cgi'
here = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--computer', required=True, type=Path)
parser.add_argument('--python-wasi', required=True, type=Path, help='Directory with python.wasm and lib/')
parser.add_argument('--output', required=True, type=Path, help='A new directory')
parser.add_argument('--live', action='store_true', help='Request the four dated scenes from NASA')
args = parser.parse_args()
output = args.output.resolve()
if any(':' in str(p) for p in [output, here]):
    parser.error('Choose mount paths without a colon.')
output.mkdir(parents=True, exist_ok=False)
runtime = args.python_wasi.resolve()
command = [str(args.computer.resolve()), '-mount', str(output) + ':/work', '-mount', str(here) + ':/recipe:ro',
           '-mount', str(here / 'scenes') + ':/scenes:ro', '-python', str(runtime / 'python.wasm'),
           '-python-lib', str(runtime / 'lib'), '-timeout', '3m']
source = '/work' if args.live else '/scenes'
if args.live:
    command.extend(['-allow-url', api])
script = ['set -eo pipefail']
for record in json.loads((here / 'scenes/receipts.json').read_text()):
    day = record['date']
    filename = day + '.jpg'
    if args.live:
        query = urlencode(dict(SERVICE='WMS', VERSION='1.1.1', REQUEST='GetMap',
                               LAYERS='MODIS_Terra_CorrectedReflectance_TrueColor', STYLES='',
                               SRS='EPSG:4326', BBOX='-92,40,-75,50', WIDTH=680, HEIGHT=400,
                               FORMAT='image/jpeg', TIME=day))
        script.append('curl -fsS ' + shlex.quote(api + '?' + query) + ' > ' + shlex.quote(filename))
    script.append('identify ' + shlex.quote(source + '/' + filename))
script.append('python3 /recipe/assemble.py ' + source)
guest = subprocess.run(command + ['-c', '\n'.join(script)], check=True, text=True, stdout=subprocess.PIPE, timeout=195)
print(guest.stdout, end='')
receipts = json.loads((output / 'receipts.json').read_text())
html = (output / 'desk.html').read_text()
images = re.findall(r'data:image/jpeg;base64,([A-Za-z0-9+/=]+)', html)
for record, inline in zip(receipts, images, strict=True):
    filename = record['date'] + '.jpg'
    original = ((output if args.live else here / 'scenes') / filename).read_bytes()
    assert base64.b64decode(inline, validate=True) == original
    assert hashlib.sha256(original).hexdigest() == record['sha256']
    assert source + '/' + filename + ' JPEG 680x400' in guest.stdout
print(f'Verified the dimensions, digest and embedded bytes of all {len(images)} images.')
