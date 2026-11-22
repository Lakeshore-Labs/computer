# Computer field guide

[Download the visual guide for offline use](offline-guide/README.md) · [Browse all 172 exhibits](offline-guide/catalog.md)

## What crosses the MCP connection?

A client lists four tools, runs a program beside a 50,000-row upstream timetable, then retrieves the chart. Follow the outer calls, the generated Python bindings and the shared filesystem in one measured run.

[![Recorded data flow through Computer](four-tools/sample/trace.png)](four-tools/README.md)

[Follow the execution and replay it](four-tools/README.md)

## From a tool response to a ferry report

Which route had the most boardings? A single shell script reads 24 departures through an MCP tool, totals the passengers in SQLite and draws a chart. The host runs the Filesystem MCP. Computer runs the shell, SQLite and the chart command. The only mounted host folder holds the output.

[![Four frames showing the timetable, MCP records, SQLite result and chart](ferry/media/ferry-story.gif)](ferry/README.md)

[Replay the ferry report](ferry/README.md)

## A dispatch packet from the same data

The agent's computer can also produce the files people use at their desks. Print the one-page ferry ledger or open a workbook with formulas and a departure chart.

[![The ferry dispatch packet](dispatch/media/cover.png)](dispatch/README.md)

[Open the dispatch packet and replay it](dispatch/README.md)

## Seven servers in one shell

Read a file, inspect a commit, convert a time, query a graph, record milestones, fetch a page and save a binary image response in one session.

[![Actual results from seven MCP servers](reference-servers/media/seven-servers.png)](reference-servers/README.md)

[Run the seven reference servers](reference-servers/README.md)

## A week beneath our feet

Two public data sources become a map inside Computer. Download the SVG and hover over a point in a browser to read its location and magnitude.

[![Global earthquake atlas drawn from the USGS weekly feed](earthquake-atlas/media/atlas.png)](earthquake-atlas/README.md)

[Open the atlas and draw a fresh one](earthquake-atlas/README.md)

## Pass a file between MCP clients

MCP Inspector writes a notice, mcporter edits it, and a Python client reads and checks it. Three separate Computer processes share one mounted folder.

[![Inspector reading the updated notice](client-handoff/media/inspector-read.png)](client-handoff/README.md)

[Replay the client handoff](client-handoff/README.md)

## What if two dispatchers edit the same timetable?

Three Computer instances start from one read-only timetable. Two make independent changes to the same path and run the same shell command over their own version.

[![Timetables in separate copy-on-write workspaces](what-if-workspaces/sample/comparison.png)](what-if-workspaces/README.md)

[Run the Go embedding example](what-if-workspaces/README.md)

## How much of the data reaches Gemini?

A full-file prompt and a Computer-filtered prompt answered the same question about 1,440 trips. Compare the actual provider-reported input tokens and inspect the fixed-query experimental design.

[![Prompt tokens before and after selecting the row in Computer](model-input/sample/tokens.png)](model-input/README.md)

[Inspect the measurements or replay the experiment](model-input/README.md)

## Repair the bakery sales board

Computer reads a mobile browser audit, repairs the page in a mounted directory and sends Chrome to the new URL. Both original findings clear on the second audit.

[![Chrome's mobile audit before and after the guest edits the bakery page](bakery-repair/sample/audit.png)](bakery-repair/README.md)

[Replay the edit and inspect both reports](bakery-repair/README.md)

## What changed between rehearsal and opening night?

Computer reads two tagged lighting plans as separate filesystems. The show still runs for 133 seconds, but four cues changed duration and two changed color.

[![Cue timing and colors in the two Git tags](stage-lighting/sample/cues.png)](stage-lighting/README.md)

[Inspect the cue sheet and replay the comparison](stage-lighting/README.md)

## Compare the Great Lakes from orbit

Four NASA Terra MODIS observations became an offline application inside Computer. Switch the dates and move the divider to inspect the same landscape.

[![Actual browser capture of the Great Lakes comparison desk](earth-observer/sample/desk.jpg)](earth-observer/README.md)

[Download the viewer and replay the workflow](earth-observer/README.md)

## Count the official MCP registry

Computer paged through 70,222 registrations and charted how providers distribute their servers. Replay all 703 captured page counts offline or collect a bounded live sample.

[![Registry registrations grouped by declared distribution](registry-census/sample/distribution.png)](registry-census/README.md)

## Publish a revised festival program

The box office uses TypeScript, the projectionist uses Java and a calendar subscriber uses Python. Their three independent Computer sessions publish the lobby board and verify the revised calendar. A browser downloads the new edition.

[![The revised screening times displayed in a browser](festival-calendar/sample/amended.png)](festival-calendar/README.md)

## Bring two museum collections into the same room

A native rclone filesystem combines the Getty and Cleveland collections inside Computer. The guest produces a portable two-painting study with credited images and collection links.

[![Vincent van Gogh’s paintings from Cleveland and the Getty](museum-study/sample/study.jpg)](museum-study/README.md)

## Run the offline workbook

Pick from 34 standalone shell recipes. Build images and charts, read or write documents, query a million SQLite rows and turn a timetable into sound. Every run starts with a new writable directory.

[![Four products from an actual complete workbook run](offline-workbook/workbook.jpg)](offline-workbook/README.md)

## Issue a printable lost-property ticket

Computer issues a PDF and QR for a fictional library desk. A Rust MCP client reads the original and rotated image, then independently scans a rendering of the printable ticket.

[![A printed ticket for a red wool scarf](claim-ticket/sample/paper.png)](claim-ticket/README.md)

## Take four moons into the classroom

A Wikidata MCP query, a JPL measurement table and a credited spacecraft image become an offline e-book. Open the actual EPUB, compare the moon sizes and follow the chapter navigation on a phone.

[![Four moons of Jupiter open in an EPUB reader](moon-almanac/sample/cover.png)](moon-almanac/README.md)

## Deliver two editions from a registry

A library reschedules its reading group. Computer publishes both program editions into an OCI layout. ORAS pulls two distinct calendars and one shared book from a real local registry.

[![The reading-room program with its amended time](reading-registry/sample/program.png)](reading-registry/README.md)

## Explore the Oslofjord in relief

Rotate two solids made from the same real elevation grid, then move through their outer walls at the same height. The stock Computer guest creates the STL files and both offline readers. [Download and explore the workshop](terrain-workshop/README.md)

[![Oslofjord terrain open in the 3D reader](terrain-workshop/sample/desktop.png)](terrain-workshop/README.md)

## Find ten bikes in Oslo

Explore three observed Oslo City Bike feeds, open the GIS files, or change the size of a group meeting at Jernbanetorget. Computer writes a shapefile, reads back a GDAL-projected GeoPackage and calculates which nearby station could serve everyone. [Open the two offline readers](oslo-bike-desk/README.md)

[![Three sites with enough bikes for the group](oslo-bike-desk/sample/meeting.png)](oslo-bike-desk/README.md)

## Inspect three satellite windows over Venice

Open San Marco, Burano or Lido and inspect a real satellite pixel. The guest requested 3.65% of the remote GeoTIFF using four HTTP range reads. A separate GDAL reader matched every pixel. [Open the offline inspector](lagoon-windows/README.md)

[![A three-window satellite field reader over the Venetian Lagoon](lagoon-windows/sample/desktop.png)](lagoon-windows/README.md)

### Search 94 recorded hosted MCP cases

The [hosted fieldwork reader](hosted-fieldwork/) records 90 passing cases across 86 distinct endpoints and exposes the four missed checks. Replay a museum journey, a Warsaw sky brief or an independently checked Van Gogh record.

### Build a designer’s palette from satellite pixels

The [Venetian Lagoon palette](lagoon-palette/) uses the official Swift MCP client and all four Computer tools. Inspect six sampled pixels and take the colors into a native macOS list, GIMP, Adobe Swatch Exchange or CSS.

### Recover last week’s library opening hours

The [restored reading room](restored-library/) compares two actual encrypted restic snapshots. Computer generated the editions, then a fresh guest read the restored files and published a comparison with both schedules available to download.

### Publish parts for a flute and a B-flat clarinet

[Last Boat Home](rehearsal-room/) registers a custom WASI command in Computer. Two shells turn one concert-pitch score into four files. Switch instruments in the offline stand, play the 20-second tune or open the parts in a music editor.

### Fit seven ushers after an offline wardrobe recount

The [browser wardrobe](browser-wardrobe/) keeps its files in Chrome’s origin-private file system. Computer reads them through MCP, calculates the fitting list and writes it back. The example checks the saved files after a complete browser restart.
