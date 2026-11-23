"""Verify the offline bundle and print its exhibit index as Markdown."""
import argparse
import hashlib
from html.parser import HTMLParser
import json
from pathlib import Path, PurePosixPath
import posixpath
from urllib.parse import unquote, urlsplit
from zipfile import ZipFile

class Links(HTMLParser):
    def __init__(self):
        super().__init__()
        self.targets = []

    def handle_starttag(self, tag, attrs):
        for key, value in attrs:
            if key in ('href', 'src', 'poster') and value:
                link = urlsplit(value)
                if not link.scheme and not link.netloc and link.path:
                    self.targets.append(unquote(link.path))

def verify(archive):
    members = archive.namelist()
    names = set(members)
    if len(names) != len(members):
        raise ValueError('Archive contains duplicate paths')
    if archive.testzip() is not None:
        raise ValueError('Archive integrity check failed')
    if not {'index.html', 'limits.html', 'search.js', 'tour.css'} <= names:
        raise ValueError('Archive is missing an entry point')
    exhibits = []
    for name in sorted(names):
        path = PurePosixPath(name)
        if path.is_absolute() or '..' in path.parts:
            raise ValueError(f'Unexpected archive path {name}')
        if path.suffix == '.html':
            links = Links()
            links.feed(archive.read(name).decode())
            for relative in links.targets:
                target = posixpath.normpath(posixpath.join(str(path.parent), relative))
                if target not in names:
                    raise ValueError(f'{name} links to a missing file: {target}')
        if path.parent == PurePosixPath('cases') and path.suffix == '.html':
            item = json.loads(archive.read(f'evidence/{path.stem}.json'))
            exhibits.append((name, item))
    return exhibits

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('archive', nargs='?', type=Path,
                        default=Path(__file__).with_name('computer-field-guide.zip'))
    args = parser.parse_args()
    with ZipFile(args.archive) as archive:
        exhibits = verify(archive)
        digest = hashlib.sha256(args.archive.read_bytes()).hexdigest()
        print('# Offline exhibit index\n')
        print(f'{len(exhibits)} exhibits · {len(archive.namelist())} files · {args.archive.stat().st_size:,} bytes\n')
        print(f'Archive SHA-256: `{digest}`\n')
        print('The paths below are relative to the extracted archive. Public replay links open on GitHub.')
        groups = {}
        for path, item in exhibits:
            groups.setdefault(item['group'], []).append((path, item))
        for group in ['Start here', 'Runtime behavior', 'Recipes', 'File stores', 'Local MCP workflows', 'Hosted MCP compatibility']:
            print(f'\n## {group}\n')
            for path, item in sorted(groups.pop(group), key=lambda entry: entry[1]['title'].casefold()):
                replay = f" · [Replay]({item['replay']})" if item.get('replay') else ''
                print(f"- **{item['title']}** — {item['summary']} · `{path}`{replay}")
        if groups:
            raise ValueError(f'Unlisted exhibit groups: {list(groups)}')

if __name__ == '__main__':
    main()
