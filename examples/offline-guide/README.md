# Take the Computer field guide offline

[![The offline field guide showing the four-tool trace and the visual workflow library](preview.png)](computer-field-guide.zip?raw=true)

[Download the field guide](computer-field-guide.zip?raw=true) · [Browse the full exhibit index on GitHub](catalog.md)

Extract the ZIP and open `index.html` in a browser. Search for a tool, file store or task. Each exhibit includes an observed result and its evidence. The collection study, festival calendars, tide listening room, terminal recordings, satellite viewer, printable dispatch sheet and Excel workbook all work offline. Public replay and attribution links open external sites.

| Try this | What Computer does | What another interface verifies |
| --- | --- | --- |
| Compare two Van Gogh paintings | Joins Getty and Cleveland files through a native rclone VFS, resizes the two images and writes an offline study. | A browser decodes the exact thumbnails in two desktop columns or a single phone column. |
| Update a festival program | TypeScript and Java clients use Computer to publish and amend a lobby page and calendar. | A read-only Python subscriber checks stable event IDs, and Chrome downloads the exact revised ICS. |
| Listen to three tide stations | Synthesizes one timed note for each observed NOAA level and packages the tracks in HTML. | A waveform reader checks note pitch against the measurements. The browser plays each track offline. |
| Repair the bakery sales board | Calls Chrome DevTools MCP and writes a repaired HTML file in WASI Python. | Lighthouse checks the original and revised pages. |
| Compare the Great Lakes in winter and summer | Fetches four dated NASA scenes and bundles them into a single HTML application. | An offline browser changes dates and moves the image divider. |
| Find a ferry trip in 1,440 records | Queries an upstream filesystem inside the shell and passes the selected row to Gemini. | Native provider usage records 55 input tokens for the selected-row prompt and 13,338 for the full-file prompt. Both answer the same trip and rider count. |
| Try two dispatch plans | Runs the same shell over separate writable in-memory filesystems with a shared base. | A Go host reopens and compares all three schedules. |
| Read a changing blob selection | Mounts a Go Cloud bucket backed by the official local Azure Blob emulator. | Independent rclone reads match the files used for both thumbnails. |

## What the numbers count

The November 22, 2026 snapshot contains 164 entries. Twenty-two featured entries tell complete stories. Seven runtime chapters cover the CLI, MCP and embedded runtimes. The library also contains 48 shell recipes, 22 storage exhibits, nine multi-tool workflows and 56 successful hosted calls to 55 distinct endpoints. Each number counts entries in that category. Several entries show the same underlying feature from different interfaces.

The scope page records the three hosted attempts with unmet checks, observed storage limitations and cases left unrun in the initial survey. Later chapters demonstrate an actual Azurite-backed Blob mount, two Git tag filesystems, an in-process rclone VFS and two chunked storage gateways. Hosted results and public datasets reflect the time of capture.

## Verify the package

The verification program uses the Python standard library. It checks archive integrity, local HTML references (including video posters), and the evidence file for every exhibit. It prints the archive digest and generates the index published alongside the download.

```sh
python3 examples/offline-guide/verify_bundle.py > /tmp/computer-exhibit-index.md
diff -u examples/offline-guide/catalog.md /tmp/computer-exhibit-index.md
```

## Connect an MCP client to the guide

![Computer searching the guide, reading the bakery audit, and saving its image](query-guide.gif)

The same ZIP can be the data source for a local MCP server. Build the server from this directory and it embeds the exhibits, receipts and downloadable documents in one binary. Start the binary over stdio with MCP Inspector or another MCP client.

```sh
CGO_ENABLED=0 go build -o ./bin/field-guide ./examples/offline-guide
```

The `search` tool finds exhibits by words in their titles, categories and descriptions. It returns up to twelve matches and the total match count. Pass an offset to read the next matches in stable ID order. The `show` tool accepts one of those IDs and returns the evidence document plus a PNG or JPEG when the exhibit has one. An MCP client can also list seven resources and read their data directly.

| Resource | Content |
| --- | --- |
| `guide://dispatch/sheet` | Printable PDF |
| `guide://dispatch/workbook` | Excel workbook |
| `guide://observations/great-lakes` | Self-contained satellite comparison HTML |
| `guide://festival/original` | Original three-screening calendar in iCalendar format |
| `guide://festival/amended` | Updated calendar with stable event UIDs and revised times |
| `guide://collections/saint-remy` | Self-contained museum study with both collection images and credits |
| `guide://observations/pacific-listening-room` | Self-contained player with three recorded tide tracks and observation tables |

MCP Inspector can list the resources directly with `npx -y @modelcontextprotocol/inspector@2.13.0 --cli ./bin/field-guide --method resources/list`.

To explore it inside Computer, start the field-guide server as an upstream MCP. The following example assumes a repository path without spaces. Use a Computer JSON config with separate command and argument fields for other paths.

```sh
CGO_ENABLED=0 go build -o ./bin/computer ./cmd/computer
mkdir -p ./bin/guide-work
./bin/computer -mount ./bin/guide-work:/work -mcp "guide=$PWD/bin/field-guide" \
  -c "guide search --query 'bakery chrome' | jq -r '.items[].title'"
```

The replay uses the Python MCP SDK to read the PDF and compare the two calendars, museum study and tide player byte for byte with the archive. A second connection lists Computer’s four tools, calls its shell to select a bakery exhibit and save the upstream image, then reads the resulting PNG. The replay compares both files byte for byte against the archive. The recorded PDF is 1,983 bytes. The PNG returned through Computer is 100,352 bytes. Run it with a fresh output directory. [Captured MCP receipt](mcp-receipt.json)

```sh
uv run --with mcp==2.3.0 python examples/offline-guide/replay.py \
  --guide ./bin/field-guide --computer ./bin/computer --output ./bin/guide-result
```
