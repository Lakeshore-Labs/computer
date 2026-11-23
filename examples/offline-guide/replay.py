"""Find an exhibit, read its PDF as an MCP resource, and retrieve a preview through Computer."""
import argparse
import asyncio
import base64
from contextlib import AsyncExitStack, asynccontextmanager
import hashlib
import json
from pathlib import Path
from zipfile import ZipFile
from urllib.parse import quote

from mcp import ClientSession, StdioServerParameters, stdio_client

@asynccontextmanager
async def connect(program, *arguments):
    params = StdioServerParameters(command=str(program.resolve()), args=list(arguments))
    async with stdio_client(params) as (reader, writer), ClientSession(reader, writer) as session:
        await session.initialize()
        yield session

async def replay(guide, computer, output):
    output.mkdir(parents=True, exist_ok=False)
    async with AsyncExitStack() as stack:
        archive = stack.enter_context(ZipFile(Path(__file__).with_name('computer-field-guide.zip')))
        expected_pdf = archive.read('assets/dispatch.pdf')
        expected_image = archive.read('assets/repair.png')
        receipts = {Path(name).stem: archive.read('evidence/' + Path(name).stem + '.json')
                    for name in archive.namelist() if name.startswith('cases/') and name.endswith('.html')}
        assets = {
            'guide://assets/' + quote(name.removeprefix('assets/'), safe='/'): name
            for name in archive.namelist() if name.startswith('assets/') and not name.endswith('/')
        }
        documents = {
            uri: (kind, archive.read(member)) for uri, kind, member in [
                ('guide://festival/original', 'text/calendar', 'assets/original.ics'),
                ('guide://festival/amended', 'text/calendar', 'assets/amended.ics'),
                ('guide://collections/saint-remy', 'text/html', 'assets/museum-study.html'),
                ('guide://observations/pacific-listening-room', 'text/html', 'assets/listen-to-the-pacific.html'),
            ]
        }
        direct = await stack.enter_async_context(connect(guide))
        operations = sorted(t.name for t in (await direct.list_tools()).tools)
        assert operations == ['search', 'show']
        page = await direct.list_resources()
        listed = list(page.resources)
        while page.next_cursor:
            page = await direct.list_resources(cursor=page.next_cursor)
            listed.extend(page.resources)
        resources = sorted(str(r.uri) for r in listed)
        asset_uris = {uri for uri in resources if uri.startswith('guide://assets/')}
        assert asset_uris == assets.keys()
        asset_bytes = 0
        for uri in sorted(asset_uris):
            result = await direct.read_resource(uri)
            assert len(result.contents) == 1
            content = result.contents[0]
            data = content.text.encode('utf-8') if hasattr(content, 'text') else base64.b64decode(content.blob, validate=True)
            assert str(content.uri) == uri and data == archive.read(assets[uri]), uri
            asset_bytes += len(data)
        templates = (await direct.list_resource_templates()).resource_templates
        template = next(t for t in templates if t.uri_template == 'guide://exhibits/{id}')
        assert template.mime_type == 'application/json'
        for exhibit_id, expected in receipts.items():
            uri = template.uri_template.replace('{id}', exhibit_id)
            response = await direct.read_resource(uri)
            assert len(response.contents) == 1
            item = response.contents[0]
            assert str(item.uri) == uri and item.mime_type == 'application/json'
            assert item.text.encode('utf-8') == expected
        document_checks = {}
        for uri, (mime, expected) in documents.items():
            assert uri in resources
            result = await direct.read_resource(uri)
            assert len(result.contents) == 1
            content = result.contents[0]
            assert content.mime_type == mime and content.text.encode('utf-8') == expected
            document_checks[uri] = {'bytes': len(expected), 'sha256': hashlib.sha256(expected).hexdigest()}
        pdf = await direct.read_resource('guide://dispatch/sheet')
        sheet = base64.b64decode(pdf.contents[0].blob, validate=True)
        assert sheet == expected_pdf
        (output / 'dispatch.pdf').write_bytes(sheet)
    # The upstream process receives no host mounts. Its catalog is embedded at build time.
    config = output / 'computer.json'
    config.write_text(json.dumps({'mounts': [f'{output}:/work'],
                                 'mcpServers': {'guide': {'command': str(guide.resolve())}}}))
    async with connect(computer, '-config', str(config), 'mcp') as outer:
        tools = sorted(t.name for t in (await outer.list_tools()).tools)
        assert tools == ['bash', 'edit', 'read', 'write']
        command = """set -eo pipefail
id=$(guide search --query 'bakery chrome' | jq -er '.items[] | select(.id == "repair") | .id')
guide show --id "$id" > combined.txt
picture=$(tail -n 1 combined.txt)
cp "$picture" bakery.png
sed '$d' combined.txt > evidence.json
jq -r '.evidence.after.scores.SEO' evidence.json
identify bakery.png"""
        result = await outer.call_tool('bash', {'command': command})
        assert not result.is_error
        answer = result.content[0].text.strip()
        assert answer.splitlines() == ['100', 'bakery.png PNG 1080x650'], answer
        read = await outer.call_tool('read', {'path': 'bakery.png'})
        images = [block for block in read.content if block.type == 'image']
        assert not read.is_error and len(images) == 1 and images[0].mime_type == 'image/png'
        image = base64.b64decode(images[0].data, validate=True)
        assert image == expected_image == (output / 'bakery.png').read_bytes()
    receipt = {'guide_tools': operations, 'resource_uris': [uri for uri in resources if uri not in assets], 'exact_downloadable_assets': len(assets), 'downloadable_asset_bytes': asset_bytes, 'exhibit_template': template.uri_template, 'exact_exhibit_receipts': len(receipts), 'documents': document_checks, 'outer_tools': tools,
               'pdf_bytes': len(sheet), 'pdf_sha256': hashlib.sha256(sheet).hexdigest(),
               'guest_answer': answer, 'image_bytes': len(image), 'image_sha256': hashlib.sha256(image).hexdigest()}
    (output / 'receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(json.dumps(receipt, indent=2))

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--guide', required=True, type=Path)
    parser.add_argument('--computer', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    if ':' in str(args.output.resolve()):
        parser.error('Choose an output path without a colon for the Computer mount.')
    asyncio.run(replay(args.guide, args.computer, args.output.resolve()))
