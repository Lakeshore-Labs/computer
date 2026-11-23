"""Run inside Computer. The generated browser module calls Chrome DevTools MCP."""
import base64
import hashlib
from html import escape
import json
from pathlib import Path
import sys
from tools import browser

base, host_output = sys.argv[1:]
work = Path('/work')
site = work / 'site'
description = 'Synthetic bakery sales board computed in DuckDB and drawn in SVG.'
markup = f'<meta name="description" content="{description}"><link rel="icon" href="data:,">'

def audit(label, filename):
    browser.navigate_page(page_id=1, type='url', url=f'{base}/{filename}')
    browser.emulate(page_id=1, viewport='390x844x2,mobile,touch')
    browser.wait_for(page_id=1, text=['4,800'])
    browser.take_screenshot(page_id=1, file_path=f'{host_output}/{label}.png', full_page=True)
    browser.lighthouse_audit(page_id=1, device='mobile', mode='navigation',
                             output_dir_path=f'{host_output}/{label}')
    report = json.loads((work / label / 'report.json').read_text())
    checks = {name: report['audits'][name]['score'] for name in ['meta-description', 'errors-in-console']}
    return {
        'file': filename,
        'sha256': hashlib.sha256((site / filename).read_bytes()).hexdigest(),
        'lighthouse': report['lighthouseVersion'],
        'scores': {group['title']: round(100 * group['score'])
                   for group in report['categories'].values() if group['score'] is not None},
        'checks': checks,
    }

before = audit('before', 'index.html')
source = (site / 'index.html').read_text()
assert source.count('<title>') == 1 and markup not in source
(site / 'index-v2.html').write_text(source.replace('<title>', markup + '<title>', 1))
after = audit('after', 'index-v2.html')
assert before['checks']['meta-description'] == 0
assert all(value == 1 for value in after['checks'].values())
assert json.loads((site / 'data.json').read_text())['kpi']['orders'] == 4800
(work / 'result.json').write_text(json.dumps({'before': before, 'after': after, 'orders': 4800}, indent=2) + '\n')

# Assemble the comparison in the guest and let Chrome photograph the actual report.
rows = ''.join(f'<tr><th>{escape(group)}</th><td>{score}</td><td>{after["scores"][group]}</td></tr>'
               for group, score in before['scores'].items())
picture = base64.b64encode((work / 'after.png').read_bytes()).decode()
report_page = f"""<!doctype html><html lang="en"><meta charset="utf-8"><title>Bakery site audit</title>
<style>*{{box-sizing:border-box}}body{{margin:0;background:#f6f0df;color:#352619;font:20px/1.4 system-ui}}
main{{padding:48px;width:1080px}}header{{font:18px ui-monospace,monospace;border-bottom:1px solid #8c755c;padding-bottom:12px}}
h1{{font:64px/1 Georgia,serif;letter-spacing:-2px;margin:30px 0 14px}}p{{max-width:70ch}}
section{{display:grid;grid-template-columns:3fr 2fr;gap:36px;align-items:start}}table{{border-collapse:collapse;width:100%}}
th,td{{padding:13px 8px;border-bottom:1px solid #ae9c86;text-align:right}}th:first-child{{text-align:left}}
tbody td:last-child{{color:#286149;font-weight:700}}figure{{margin:0}}img{{width:100%;height:225px;object-fit:cover;object-position:top;display:block;border:1px solid #b4a183}}
figcaption,footer{{font:15px/1.5 ui-monospace,monospace;margin-top:10px}}footer{{border-top:1px solid #8c755c;padding-top:14px}}</style>
<main><header>MERIDIAN BAKERIES / WEBSITE AUDIT</header><h1>Two repairs to the sales board.</h1>
<p>Computer added the page description and favicon. Chrome audited both versions of the same 4,800-order sales board.</p>
<section><table><thead><tr><th>Mobile Lighthouse</th><th>Before</th><th>After</th></tr></thead><tbody>{rows}</tbody></table>
<figure><img alt="Browser capture of the bakery board" src="data:image/png;base64,{picture}"><figcaption>Detail of the repaired sales board</figcaption></figure></section>
<footer>Chrome DevTools MCP · Lighthouse {escape(after['lighthouse'])} · synthetic order data</footer></main></html>"""
(site / 'audit.html').write_text(report_page)
browser.emulate(page_id=1, viewport='1080x650x1')
browser.navigate_page(page_id=1, type='url', url=base + '/audit.html')
browser.take_screenshot(page_id=1, file_path=host_output + '/audit.png', full_page=True)
