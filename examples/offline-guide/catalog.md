# Offline exhibit index

154 exhibits · 398 files · 6,602,047 bytes

Archive SHA-256: `3178decac916bbc7c89c7ff5bb66c6ffede079162041d7692e2e00c67f2b209b`

The paths below are relative to the extracted archive. Public replay links open on GitHub.

## Start here

- **Build a tide record over time** — Each Computer shell run appends readings from three NOAA stations to a persistent mount. Another Computer opens those captures read-only and uses WASI Python and SQLite to build this time series. · `cases/tides.html`
- **Compare four Great Lakes satellite scenes offline** — Computer fetched four dated NASA GIBS scenes and bundled them into a single offline viewer. Select dates and slide the divider across the same geographic frame. · `cases/lakes.html`
- **Compare the same satellite file across two clouds** — An allowlisted shell reads a NOAA object from Amazon S3 and Google Cloud Storage. The guest computes a checksum for each copy. An independent host read compares all bytes. A third shell call reads a public blob on Azure. · `cases/cloud.html`
- **Continue a Gemini session in a new process** — A Go host supplies Computer’s Environment to the native Google model client. Gemini writes the route totals. A new host process resumes the saved ACP session and asks Computer to turn those totals into a PNG. · `cases/native-gemini.html`
- **Count the books in an Appbox catalog** — Computer called a live local bookcatalog application, counted books by author, and returned the chart. Separate MCP and PostgreSQL reads verified the twelve fictional books. · `cases/library.html`
- **Draw proportional bars** — With a truncated axis the smallest route looked almost empty. The revised bar command puts zero on the value axis for wholly positive or wholly negative values. · `cases/baseline.html` · [Replay](https://github.com/Lakeshore-Labs/computer/pull/18)
- **Follow 50,000 trips through four tools** — Two upstream servers supply CSV records and a binary image. Computer's WASI Python invokes both tools and writes SQLite. The shell builds the chart and an outer client reads the PNG. · `cases/trace.html` · [Replay](https://github.com/Lakeshore-Labs/computer/pull/16)
- **Follow a changing Azure Blob selection** — A Go host mounted an Azure Blob bucket backed by local Azurite. The guest made a thumbnail, then read a new selection from the same live mount after the host updated the blob. · `cases/blob.html`
- **Hand a mounted file to another client** — Inspector writes a notice. mcporter edits it. A new Python SDK client reads the exact result and checks the shell checksum. Each client starts a separate Computer process. · `cases/handoff.html` · [Replay](https://github.com/Lakeshore-Labs/computer/pull/17)
- **Measure how much of a file reaches Gemini** — An actual filesystem MCP supplied the same CSV to two clients. A fixed query in Computer selected the matching row. Gemini returned the same answer with 13,338 and 55 prompt tokens on the largest input. · `cases/prompt.html` · [Replay](https://github.com/Lakeshore-Labs/computer/pull/21)
- **Print a dispatch sheet or open the workbook** — WASI Python produced a one-page PDF and an Excel workbook with formulas and a chart. Computer reopened the packet and rendered the printed page. · `cases/packet.html` · [Replay](https://github.com/Lakeshore-Labs/computer/pull/12)
- **Repair a bakery site and recheck it in Chrome** — A bakery editor publishes a repaired sales board. Computer calls Chrome DevTools MCP, edits the HTML in WASI Python and assembles the report. Chrome independently checks both pages. · `cases/repair.html` · [Replay](https://github.com/Lakeshore-Labs/computer/pull/22)
- **Sit down at the Computer prompt** — A real pseudo-terminal session exercises command and file completion, tool arguments, history, and the working directory. The embedded agent segment uses a deterministic model fixture. · `cases/terminal.html`
- **Try two edits to the same timetable** — A Go application supplies a read-only base and a writable memory layer per Computer. An edit at noon and an edit in the evening coexist. Each shell sees its own schedule. · `cases/plans.html` · [Replay](https://github.com/Lakeshore-Labs/computer/pull/19)
- **Turn a tool result into a browser artifact** — DuckDB MCP aggregates synthetic orders. Computer passes the result into a local dashboard. Playwright MCP operates the browser and captures the dashboard. The host assembles eleven real browser captures into an animation. · `cases/browser.html`

## Runtime behavior

- **Discover a new command between calls** — A fixture MCP advertises a new notes export tool. On the next execution Computer discovers it and makes it available as a shell command. · `cases/changed-tools.html`
- **Give two jobs one WebAssembly slot** — A Go host starts finite guest jobs with controlled inputs. With one slot, an arriving job reaches its own deadline in the queue. When the host compiles the module with two slots, both guests enter before either is released. · `cases/concurrency.html`
- **Pipe a script, inputs and deadlines through Computer** — The CLI reads a script on stdin, passes process arguments, preserves exit codes and applies call timeout limits. A Go host exercises the public package surface. · `cases/stream-input.html`
- **Set the number of Python workers** — The same bounded pipeline runs with one, two and unlimited Python workers. Recorded guest timestamps show which programs overlap. A separate finite loop exercises the opt-in runtime mode. · `cases/python-workers.html`
- **Watch an agent compact a conversation** — The real embedded agent calls a deterministic loopback model. A smaller context triggers a recorded summarization request. A resumed turn reads the resulting summary. · `cases/agent-context.html`

## Recipes

- **Aggregate three routes directly from a CSV** — Shell and SQL · `cases/recipe-xan-profile.html`
- **Archive the timetable and verify its contents** — Shell and SQL · `cases/recipe-archive-checksum.html`
- **Build a workbook with formulas and a chart, then read it back** — Documents · `cases/recipe-xlsx-brief.html`
- **Call a tool with a typed JSON object** — MCP reference servers · `cases/recipe-mcp-json.html`
- **Call multiple MCP tools from one Python process** — MCP reference servers · `cases/recipe-python-mcp.html`
- **Compare ferry routes by departure** — Charts and images · `cases/recipe-chart-bar.html`
- **Convert Markdown to HTML and back** — Documents · `cases/recipe-markdown-roundtrip.html`
- **Convert the morning shift between two port offices** — MCP reference servers · `cases/recipe-reference-clock.html`
- **Convert the public timetable to Markdown** — Documents · `cases/recipe-notice-to-markdown.html`
- **Create a database in Python and query it from the shell** — Shell and SQL · `cases/recipe-sqlite-handoff.html`
- **Crop and rotate part of the transit map** — Charts and images · `cases/recipe-image-cut.html`
- **Explore the sections of a public repository wiki** — Hosted MCP · `cases/recipe-deepwiki.html`
- **Extract and render one page from a PDF** — Documents · `cases/recipe-pdf-split.html`
- **Fetch one published page with a URL allowance** — Runtime · `cases/recipe-curl-allowed.html`
- **Find departures carrying at least 45 riders** — Shell and SQL · `cases/recipe-route-selector.html`
- **Find the busiest departure across three routes** — Shell and SQL · `cases/recipe-awk-peak.html`
- **Flatten a transparent ferry icon onto a chosen background** — Charts and images · `cases/recipe-image-transparency.html`
- **Flip a route map on its horizontal and vertical axes** — Charts and images · `cases/recipe-image-mirror.html`
- **Hear eight departures as a three-note harbor score** — Field notes · `cases/recipe-sonify.html`
- **Inspect the first two records returned by the filesystem tool** — MCP reference servers · `cases/recipe-tool-input.html`
- **Join route names and capacities across formats** — Shell and SQL · `cases/recipe-join-routes.html`
- **Keep a small route graph in the Memory MCP** — MCP reference servers · `cases/recipe-reference-memory.html`
- **Keep the source timetable read-only** — File stores · `cases/recipe-readonly.html`
- **List the command binaries available in this mount** — Runtime · `cases/recipe-shell-inventory.html`
- **Make a WebP map small enough for an email** — Charts and images · `cases/recipe-image-thumbnail.html`
- **Plot a day of passenger traffic** — Charts and images · `cases/recipe-chart-line.html`
- **Publish a daylight-saving-aware departure time** — Python · `cases/recipe-time-zones.html`
- **Rank ferry traffic with a SQL window function** — Shell and SQL · `cases/recipe-sqlite-window.html`
- **Read a public page through the Fetch MCP** — MCP reference servers · `cases/recipe-reference-fetch.html`
- **Read gzipped manifests without expanding them to disk** — Python · `cases/recipe-compressed-stream.html`
- **Read resource URIs returned by an MCP tool** — MCP reference servers · `cases/recipe-mcp-links.html`
- **Read the history of the synthetic timetable** — MCP reference servers · `cases/recipe-reference-git.html`
- **Read the largest ferry capacity from YAML** — Shell and SQL · `cases/recipe-service-yaml.html`
- **Record two operations-planning milestones** — MCP reference servers · `cases/recipe-reference-sequence.html`
- **Render a shareable service notice from YAML** — Documents · `cases/recipe-template-report.html`
- **Search a PDF briefing through SQLite full-text indexing** — Documents · `cases/recipe-library-search.html`
- **Show each route’s passenger counts by departure** — Charts and images · `cases/recipe-chart-scatter.html`
- **Show the distribution of Pier passengers by time** — Charts and images · `cases/recipe-chart-pie.html`
- **Summarize a million trips before returning an answer** — Field notes · `cases/recipe-million-records.html`
- **Summarize ridership with the Python standard library** — Python · `cases/recipe-python-statistics.html`
- **Transcode a map to GIF, TIFF and BMP** — Charts and images · `cases/recipe-image-formats.html`
- **Turn a Word briefing into text and HTML** — Documents · `cases/recipe-docx-reading.html`
- **Turn an MCP image response into a mounted PNG** — MCP reference servers · `cases/recipe-mcp-binary.html`
- **Typeset a two-page operations brief inside WebAssembly** — Documents · `cases/recipe-pdf-brief.html`
- **Use the structured result of a simulated forecast** — MCP reference servers · `cases/recipe-mcp-schema.html`
- **Which ferry route carried the most riders?** — Field notes · `cases/recipe-ferry-report.html`
- **Write a vector chart to standard output** — Charts and images · `cases/recipe-chart-vector.html`
- **Write and validate a TOML service manifest** — Python · `cases/recipe-toml-roundtrip.html`

## File stores

- **CLI binary over staged S3 data, results synced to WebDAV** — bin/computer-static (CLI, revision ea7bf235) + rclone copy · `cases/store-cli-staging.html`
- **CLI binary: read-write and :ro host mounts** — bin/computer-static (CLI, revision ea7bf235) · `cases/store-cli-mounts.html`
- **Copy-on-write over a host directory** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-copy-on-write.html`
- **Cross-backend story: ZIP → S3 → WebDAV chart → SFTP → SQLite manifest** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-story.html`
- **Editable ZIP: copy-on-write over zipfs, re-packed** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-zip-overlay.html`
- **Git: memory mount exported as a commit** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-git-export.html`
- **Host directory, read-write** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-host.html`
- **io/fs: files compiled into the binary (embed.FS)** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-iofs-embed.html`
- **Memory filesystem** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-memory.html`
- **Read-only by Mount.ReadOnly** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-readonly-flag.html`
- **Read-only by the filesystem (afero.ReadOnlyFs)** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-readonly-fs.html`
- **S3 with a memory read cache (afero.CacheOnReadFs)** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-cache-on-read.html`
- **S3-compatible object store (+ fstat, lost-write and marker findings)** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-s3.html`
- **S3-compatible object store, second server (gofakes3) (+ leading-slash key finding)** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-s3-gofakes3.html`
- **SFTP server (+ append finding and server-side remedy)** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-sftp.html`
- **SQLite-backed store, read-only (jilio/sqlitefs)** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-sqlite-iofs.html`
- **SQLite-backed store, read-write (custom sqlfs)** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-sqlite-rw.html`
- **Sub-tree and filename filter (+ RegexpFs create finding)** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-filtered.html`
- **TAR archive as a directory (+ tarfs re-open finding)** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-tar.html`
- **WebDAV server (+ not-found/chmod findings)** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-webdav.html`
- **ZIP archive as a directory** — Computer Go library (embedder), computer.New + Computer.Exec · `cases/store-zip.html`

## Local MCP workflows

- **Chrome DevTools MCP audits, Python patches the page, re-audit, Playwright photographs the report** — chrome-devtools + playwright · `cases/studio-devtools-audit-fix.html`
- **DuckDB computes it, Playwright photographs it, Computer crops it** — motherduck + playwright · `cases/studio-pw-duckdb-dashboard.html`
- **DuckDB introspection becomes a Mermaid ER diagram** — motherduck + mermaid · `cases/studio-duckdb-mermaid-er.html`
- **Fetch MCP and Playwright disagree about the same URL (and Everything MCP typed results)** — fetcher + everything + playwright · `cases/studio-fetch-vs-browser.html`
- **Git MCP commits DuckDB snapshots on two branches and merges; Mermaid draws the history** — motherduck + gitmcp + clockmcp + mermaid + playwright · `cases/studio-git-release-history.html`
- **One memo, five formats: DOCX, PPTX, PDF, XLSX, HTML to Markdown** — markitdown + playwright · `cases/studio-markitdown-formats.html`
- **Playwright clicks 11 months; each DOM total is checked against DuckDB** — motherduck + playwright · `cases/studio-drilldown-frames.html`
- **Sequential Thinking plans, Memory MCP stores the exhibit as a graph, Mermaid draws it** — thinker + memory + mermaid + playwright · `cases/studio-memory-graph.html`
- **Window-function SQL in DuckDB becomes a Vega-Lite chart (two PNGs)** — motherduck + vegalite · `cases/studio-duckdb-vegalite.html`

## Hosted MCP compatibility

- **Ask Cohere docs about the Rerank endpoint** — Cohere docs · `cases/hosted-cohere-docs.html`
- **Code-mode API call: Bitcoin and Ethereum USD price** — CoinGecko · `cases/hosted-coingecko-price.html`
- **Community earthquake MCP result cross-checked against the USGS public GeoJSON feed (curl through Computer)** — olyport earthquake MCP vs USGS · `cases/hosted-usgs-cross-check.html`
- **Find R2 presigned-URL documentation** — Cloudflare Docs · `cases/hosted-cloudflare-docs-search.html`
- **Full-text search of Swiss Federal Supreme Court decisions** — opencaselaw.ch (Swiss court decisions) · `cases/hosted-opencaselaw-ch.html`
- **hf-fs: list trending models as structured JSON** — Hugging Face · `cases/hosted-huggingface-trending-fs.html`
- **Hub model search: most-downloaded sentiment models** — Hugging Face · `cases/hosted-huggingface-hub-search.html`
- **Hybrid search of Apple developer docs and WWDC transcripts** — Apple docs (apple-rag, third party) · `cases/hosted-apple-rag-search.html`
- **Lint a Svelte 5 component with the official autofixer, then verify the fix** — Svelte · `cases/hosted-svelte-autofixer.html`
- **List AWS regions and check regional availability** — AWS Knowledge MCP · `cases/hosted-aws-knowledge-regions.html`
- **Read-only flight search LHR to BCN on 15 Mar 2027** — Kiwi.com · `cases/hosted-kiwi-flights.html`
- **Regex-grep and search the Bun docs** — Bun docs · `cases/hosted-bun-docs.html`
- **Resolve a library id then query its docs (two-call chain)** — Context7 (Upstash) · `cases/hosted-context7-resolve-query.html`
- **Search docs for “access keys”, then read the top page back** — NEAR docs · `cases/hosted-docs-near.html`
- **Search docs for “add memories with metadata”, then read the top page back** — Mem0 docs · `cases/hosted-docs-mem0.html`
- **Search docs for “build a voice agent pipeline”, then read the top page back** — Pipecat docs · `cases/hosted-docs-pipecat.html`
- **Search docs for “chat completions streaming”, then read the top page back** — Together AI docs · `cases/hosted-docs-together.html`
- **Search docs for “crawl a website into markdown”, then read the top page back** — Firecrawl docs · `cases/hosted-docs-firecrawl.html`
- **Search docs for “create a phone call”, then read the top page back** — Retell AI docs · `cases/hosted-docs-retell.html`
- **Search docs for “create a serverless index”, then read the top page back** — Pinecone docs · `cases/hosted-docs-pinecone.html`
- **Search docs for “create a session with a proxy”, then read the top page back** — Browserbase docs · `cases/hosted-docs-browserbase.html`
- **Search docs for “create an EVM account”, then read the top page back** — Coinbase Developer Platform docs · `cases/hosted-docs-coinbase-cdp.html`
- **Search docs for “custom properties”, then read the top page back** — Helicone docs · `cases/hosted-docs-helicone.html`
- **Search docs for “define an agent with tools”, then read the top page back** — CrewAI docs · `cases/hosted-docs-crewai.html`
- **Search docs for “deploy a smart contract”, then read the top page back** — Base docs · `cases/hosted-docs-base.html`
- **Search docs for “deploy a Truss model”, then read the top page back** — Baseten docs · `cases/hosted-docs-baseten.html`
- **Search docs for “deployments”, then read the top page back** — Replit docs · `cases/hosted-docs-replit.html`
- **Search docs for “embedded replicas”, then read the top page back** — Turso docs · `cases/hosted-docs-turso.html`
- **Search docs for “function calling”, then read the top page back** — Fireworks AI docs · `cases/hosted-docs-fireworks.html`
- **Search docs for “query a collection with a metadata filter”, then read the top page back** — Chroma docs · `cases/hosted-docs-chroma.html`
- **Search docs for “search depth advanced parameter”, then read the top page back** — Tavily docs · `cases/hosted-docs-tavily.html`
- **Search docs for “search with date filters”, then read the top page back** — Exa docs · `cases/hosted-docs-exa.html`
- **Search docs for “serverless endpoint handler”, then read the top page back** — Runpod docs · `cases/hosted-docs-runpod.html`
- **Search docs for “speaker diarization”, then read the top page back** — AssemblyAI docs · `cases/hosted-docs-assemblyai.html`
- **Search docs for “streaming responses with citations”, then read the top page back** — Perplexity docs · `cases/hosted-docs-perplexity.html`
- **Search docs for “task retries and caching”, then read the top page back** — Prefect docs · `cases/hosted-docs-prefect.html`
- **Search docs for “volumes and regions”, then read the top page back** — Fly.io docs · `cases/hosted-docs-flyio.html`
- **Search docs, then read a page via the virtual docs filesystem** — LangChain Docs (Mintlify) · `cases/hosted-langchain-docs.html`
- **Search EASA air-ops rules for minimum equipment lists** — Avioverse (EASA regulations) · `cases/hosted-avioverse-easa.html`
- **Search FastMCP docs for composing servers** — FastMCP docs (Mintlify) · `cases/hosted-fastmcp-docs.html`
- **Search IAR docs then read the top page back** — IAR Systems docs · `cases/hosted-iar-docs.html`
- **Search LiveKit docs for agent handoff** — LiveKit docs · `cases/hosted-livekit-docs.html`
- **Search Mintlify docs for navigation configuration** — Mintlify docs · `cases/hosted-mintlify-docs.html`
- **Search Neon docs for branching** — Neon docs · `cases/hosted-neon-docs.html`
- **Search official Astro docs: content collection loaders** — Astro Docs · `cases/hosted-astro-docs-search.html`
- **Search official Azure docs: timer-trigger NCRONTAB** — Microsoft Learn · `cases/hosted-mslearn-search.html`
- **Search OpenAI docs for structured outputs** — OpenAI developer docs · `cases/hosted-openai-docs.html`
- **Search PubMed then fetch article metadata for the first hit** — PubMed (Anthropic-hosted connector) · `cases/hosted-pubmed-search-meta.html`
- **Search Qt docs then read the top page back** — Qt Group docs · `cases/hosted-qt-docs.html`
- **Search Resend docs for webhook verification** — Resend docs (Mintlify) · `cases/hosted-resend-docs.html`
- **Search Solana developer docs for PDA seeds** — Solana Foundation docs · `cases/hosted-solana-docs.html`
- **Search the open dataset catalog, then fetch one dataset record** — Mozilla Data Collective · `cases/hosted-mozilla-data-collective.html`
- **Semantic doc search scoped to a public repo endpoint** — GitMCP · `cases/hosted-gitmcp-go-sdk.html`
- **Semantic item search, then a SPARQL cross-check of the same entity** — Wikidata (Wikimedia) · `cases/hosted-wikidata-search-sparql.html`
- **Web search with highlights (keyless endpoint)** — Exa · `cases/hosted-exa-web-search.html`
- **Wikipedia summary through a community MCP** — Faceabot (Wikipedia wrapper) · `cases/hosted-faceabot-wikipedia.html`
