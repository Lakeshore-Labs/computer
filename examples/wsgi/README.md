# Flask in an embedded computer

This example runs a Flask app inside the same WASM-backed `Computer` used by the
library. The Go host owns the listener, the mux, the mounts, and the MCP
connection. It registers the Flask app as a regular `http.Handler` and answers
`GET /healthz` with a native Go handler.

1. The Jinja page submits a GitHub repository to a Flask route.
2. Flask imports `tools.wiki` and calls `read_wiki_structure` over MCP.
3. Flask saves the outline to SQLite on the writable `/data` mount.
4. The next Flask request reads SQLite, aggregates the history with pandas, and
   renders the lookup counts, a Seaborn chart of average outline lengths, and
   recent results in Jinja.

From the repository root, prepare a disposable working directory and start the host:

```sh
work="$(mktemp -d)"
cp -R examples/wsgi/app "$work/app"
mkdir "$work/data"
python3 -m pip install --target "$work/app/vendor" 'Flask>=3.1,<3.2'
go run ./examples/wsgi -app "$work/app" -data "$work/data"
```

Open <http://127.0.0.1:8080> and submit `pallets/flask` or
`Lakeshore-Labs/computer`. The page displays `wasi` as the Python runtime.
The first request waits for Python to compile on a cold cache. The SQLite file
persists in `$work/data` across requests and host restarts.

The default MCP server runs in the Go host and extracts headings from a live
public GitHub README. Pass `-deepwiki` to use the public DeepWiki MCP endpoint
in an environment that can reach it. Both servers expose the same Python
function, so the Flask code stays the same.
