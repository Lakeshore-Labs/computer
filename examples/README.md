# Computer field guide

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
