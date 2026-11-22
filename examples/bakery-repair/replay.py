"""Serve the synthetic bakery site on loopback and run its repair inside Computer."""
import argparse
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import shlex
import shutil
import subprocess
import tempfile
from threading import Thread

here = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--computer', required=True, type=Path)
parser.add_argument('--python-wasi', required=True, type=Path, help='Directory with python.wasm and lib/')
parser.add_argument('--devtools', required=True, type=Path, help='chrome-devtools-mcp executable')
parser.add_argument('--chrome', type=Path, help='Optional path to a Chrome executable')
parser.add_argument('--output', required=True, type=Path, help='A new directory')
args = parser.parse_args()
output = args.output.resolve()
output.mkdir(parents=True, exist_ok=False)
shutil.copytree(here / 'site', output / 'site')
source = (output / 'site/index.html').read_bytes()
handler = partial(SimpleHTTPRequestHandler, directory=output / 'site')
with tempfile.TemporaryDirectory() as temporary, ThreadingHTTPServer(('127.0.0.1', 0), handler) as server:
    base = f'http://127.0.0.1:{server.server_port}'
    options = ['--headless', '--isolated', '--no-usage-statistics', '--no-performance-crux', '--workspace', str(output)]
    if args.chrome:
        options.extend(['--executablePath', str(args.chrome.resolve())])
    config = Path(temporary) / 'config.json'
    config.write_text(json.dumps({
        'mounts': [f'{output}:/work', f'{here}:/recipe:ro'],
        'mcpServers': {'browser': {'command': str(args.devtools.resolve()), 'args': options}},
    }))
    thread = Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        runtime = args.python_wasi.resolve()
        script = shlex.join(['python3', '/recipe/repair.py', base, str(output)])
        subprocess.run([str(args.computer.resolve()), '-config', str(config), '-python',
                        str(runtime / 'python.wasm'), '-python-lib', str(runtime / 'lib'),
                        '-timeout', '4m', '-c', script], check=True, timeout=255)
    finally:
        server.shutdown()
        thread.join()
assert source == (output / 'site/index.html').read_bytes()
assert (output / 'audit.png').read_bytes().startswith(b'\x89PNG\r\n\x1a\n')
print((output / 'result.json').read_text())
