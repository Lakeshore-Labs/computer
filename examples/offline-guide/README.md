# Take the Computer field guide offline

[![The offline field guide showing the four-tool trace and the visual workflow library](preview.png)](computer-field-guide.zip?raw=true)

[Download the field guide](computer-field-guide.zip?raw=true) · [Browse the full exhibit index on GitHub](catalog.md)

Extract the ZIP and open `index.html` in a browser. Search for a tool, file store or task. Each exhibit includes an observed result and its evidence. The terminal and browser recordings, an interactive satellite viewer, a playable audio file, a printable dispatch sheet and an Excel workbook all work offline. Public replay and attribution links open external sites.

| Try this | What Computer does | What another interface verifies |
| --- | --- | --- |
| Repair the bakery sales board | Calls Chrome DevTools MCP and writes a repaired HTML file in WASI Python. | Lighthouse checks the original and revised pages. |
| Compare the Great Lakes in winter and summer | Fetches four dated NASA scenes and bundles them into a single HTML application. | An offline browser changes dates and moves the image divider. |
| Find a ferry trip in 1,440 records | Queries an upstream filesystem inside the shell and passes the selected row to Gemini. | Native provider usage records 55 input tokens for the selected-row prompt and 13,338 for the full-file prompt. Both answer the same trip and rider count. |
| Try two dispatch plans | Runs the same shell over separate writable in-memory filesystems with a shared base. | A Go host reopens and compares all three schedules. |
| Read a changing blob selection | Mounts a Go Cloud bucket backed by the official local Azure Blob emulator. | Independent rclone reads match the files used for both thumbnails. |

## What the numbers count

The November 22, 2026 snapshot contains 154 entries. Fifteen featured entries tell complete stories. Five runtime chapters cover the CLI and embedded runtimes. The library also contains 48 shell recipes, 21 storage configurations, nine multi-tool workflows and 56 successful hosted calls to 55 distinct endpoints. Each number counts entries in that category. Several entries show the same underlying feature from different interfaces.

The scope page records the three hosted attempts with unmet checks, observed storage limitations and cases left unrun in the initial survey. The featured Azure Blob exhibit extends that initial storage survey. Hosted results and public datasets reflect the time of capture.

## Verify the package

The verification program uses the Python standard library. It checks archive integrity, local HTML references (including video posters), and the evidence file for every exhibit. It prints the archive digest and generates the index published alongside the download.

```sh
python3 examples/offline-guide/verify_bundle.py > /tmp/computer-exhibit-index.md
diff -u examples/offline-guide/catalog.md /tmp/computer-exhibit-index.md
```
