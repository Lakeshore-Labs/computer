"""Seed two Git tags and check Computer's cue sheet against independent git show reads."""
import argparse
import csv
from io import StringIO
import json
import os
from pathlib import Path
import subprocess
import tempfile
from xml.etree import ElementTree

here = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--host', type=Path, required=True)
parser.add_argument('--output', type=Path, required=True)
args = parser.parse_args()
output = args.output.resolve()
output.mkdir(parents=True, exist_ok=False)
with tempfile.TemporaryDirectory() as temporary:
    repo = Path(temporary) / 'lighting'
    repo.mkdir()
    env = dict(os.environ, GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull,
               GIT_AUTHOR_NAME='Stage crew', GIT_COMMITTER_NAME='Stage crew',
               GIT_AUTHOR_EMAIL='crew@example.invalid', GIT_COMMITTER_EMAIL='crew@example.invalid',
               GIT_AUTHOR_DATE='2025-02-18T12:00:00Z', GIT_COMMITTER_DATE='2025-02-18T12:00:00Z')

    def git(*arguments):
        return subprocess.check_output(['git', '-c', 'core.hooksPath=' + os.devnull, '-C', str(repo), *arguments], env=env)

    git('init', '-q', '-b', 'main')
    tags = {}
    for name in ['rehearsal', 'opening']:
        (repo / 'cues.csv').write_bytes((here / 'fixtures' / (name + '.csv')).read_bytes())
        git('add', '--', 'cues.csv')
        git('commit', '-qm', name)
        git('tag', name)
        tags[name] = git('rev-parse', name).decode().strip()

    scribble = 'Uncommitted rehearsal scribble.\n'
    (repo / 'cues.csv').write_text(scribble)
    subprocess.run([str(args.host.resolve()), '-repo', str(repo), '-output', str(output), '-recipe', str(here)],
                   env=env, check=True, timeout=90)
    versions = {}
    for tag, filename in [('rehearsal', 'first.csv'), ('opening', 'last.csv')]:
        data = git('show', tag + ':cues.csv')
        assert (output / filename).read_bytes() == data
        versions[tag] = {row['cue']: row for row in csv.DictReader(StringIO(data.decode()))}
    first, last = versions['rehearsal'], versions['opening']
    assert first.keys() == last.keys()
    expected = [dict(cue=cue, rehearsal=first[cue]['seconds'], opening=row['seconds'],
                     first_color=first[cue]['color'], last_color=row['color']) for cue, row in last.items()]
    rows = list(csv.DictReader((output / 'comparison.csv').read_text().splitlines()))
    assert rows == expected and len(rows) == 5
    assert (repo / 'cues.csv').read_text() == scribble

    svg = ElementTree.parse(output / 'cues.svg')
    bars = svg.findall('.//{*}rect[@class="cue"]')
    expected_bars = [(7 * int(row[key]), row[color]) for row in rows
                     for key, color in [('rehearsal', 'first_color'), ('opening', 'last_color')]]
    assert [(int(bar.attrib['width']), bar.attrib['fill']) for bar in bars] == expected_bars
    checks = {'tags': tags, 'cues': len(rows), 'verified_svg_bars': len(bars),
              'changed_durations': sum(row['rehearsal'] != row['opening'] for row in rows),
              'changed_colors': sum(row['first_color'] != row['last_color'] for row in rows),
              'total_seconds': [sum(int(row[key]) for row in rows) for key in ['rehearsal', 'opening']],
              'working_tree_preserved': True, 'both_guest_files_equal_git_show': True}
    (output / 'checks.json').write_text(json.dumps(checks, indent=2) + '\n')
    print(json.dumps(checks, indent=2))
