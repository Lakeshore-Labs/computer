# computer

A sandboxed computer for an AI agent, as a Go library and one binary. It needs no Node and no container at run time, and it is built without cgo.

You give it folders and MCP servers. The agent gets a shell, SQLite and Python over those folders, and every MCP
tool appears as a shell command and as a Python function. Nothing inside reaches the network or a file outside the
folders, except through those tools.

> **Status: experimental.** Two independent reviews each found ways out of the sandbox, which are fixed and tested
> here. Expect more. Do not rely on this as your only boundary around untrusted code.

## Why

An agent that can only call tools must pass every result through the model. That is slow, costs tokens, and fails
once a result is too large to read. With a computer the agent writes a short script; the script handles the data
and only the answer returns to the model. The agent also carries four tool definitions, however many tools the
servers offer.

## Use it

```
go build -o computer ./cmd/computer

# an interactive shell over a folder
./computer -mount ./work:/home/user

# one command
./computer -mount ./work:/home/user -c 'ls | wc -l'

# a public MCP server by URL: DeepWiki, which answers questions about GitHub repositories
./computer -mount ./work:/home/user -mcp wiki=https://mcp.deepwiki.com/mcp \
    -c 'wiki --help; wiki read-wiki-structure --repo-name wazero/wazero | head'

# an MCP server started as a program: the reference "everything" server
./computer -mount ./work:/home/user -mcp 'everything=npx -y @modelcontextprotocol/server-everything' \
    -c 'everything echo --message hello'

# several servers at once, each its own command
./computer -mount ./work:/home/user -mcp wiki=https://mcp.deepwiki.com/mcp -mcp docs=https://mcp.context7.com/mcp

# as an MCP server for an agent: its tools are bash, read, edit and write
./computer -mount ./work:/home/user -mcp wiki=https://mcp.deepwiki.com/mcp mcp
```

With more than a few of either, put them in a file. `mcpServers` is the format Claude Code and Claude Desktop
read, so a `.mcp.json` you already have works as it is; `mounts` are as `-mount` takes them.

```json
{
  "mounts": ["./work:/home/user", "./docs:/reference:ro"],
  "mcpServers": {
    "wiki": {"type": "http", "url": "https://mcp.deepwiki.com/mcp", "headers": {"Authorization": "Bearer ${WIKI_TOKEN}"}},
    "everything": {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-everything"]}
  }
}
```

```
./computer -config computer.json
```

`${NAME}` in a header or an `env` value is read from the environment. Flags add to the file.

A server that needs a bearer token reads it from the environment: `COMPUTER_MCP_TOKEN_WIKI` for a server named
`wiki`, `COMPUTER_MCP_TOKEN_MY_WIKI` for `my-wiki`. A server started as a program runs on the host, outside the
sandbox, like any other program you start; it inherits the environment except the
`COMPUTER_MCP_TOKEN` variables, which its `env` can pass on by name.

For Claude Code:

```
claude mcp add computer -- /path/to/computer -mount /path/to/work -mcp wiki=https://mcp.deepwiki.com/mcp mcp
```

A folder given alone is seen in the computer at the path it has on your machine. Claude Code tells the model where
it is working, and the model will use that path inside the computer. `-mount host:guest` names another path.

From Go:

```go
c, err := computer.New(computer.Config{
    Mounts: []computer.Mount{{Guest: "/home/user", Host: dir}},
    Tools:  []tools.Source{{Name: "wiki", Session: session}}, // an MCP client session
    Python: python,                                           // from computer.LoadPython, shared by every computer
})
defer c.Close()
res, err := c.Exec(ctx, &gbash.ExecutionRequest{Script: "wiki read-wiki-structure --repo-name wazero/wazero | wc -l"})
```

A host that serves many sessions gives each its own computer with the `computers` package. A `computers.Computers`
is one `Environment` for all of them: each call goes to the computer of the session its context names, made when
that session first uses it and closed after it has been idle. You say which session a context belongs to and what
its computer is.

```go
cs := computers.New(
    func(ctx context.Context) (string, bool) { id, ok := ctx.Value(sessionKey{}).(string); return id, ok },
    func(ctx context.Context, session string) (computer.Config, error) {
        return computer.Config{Mounts: []computer.Mount{{Guest: "/home/user", Host: dirOf(session)}}}, nil
    },
    10*time.Minute, // idle time; zero keeps every computer until Close
)
defer cs.Close()
cs.End(session) // when a session is deleted
```

## Mounts

A mount is a directory of the computer. The shell and every program are given the same one, so a file one writes
another reads. It is a host directory, or anything that implements [afero](https://github.com/spf13/afero)'s
filesystem interface: memory, a database, an object store.

```go
computer.Mount{Guest: "/home/user", Host: dir}                     // a host directory
computer.Mount{Guest: "/scratch", FS: afero.NewMemMapFs()}         // kept in memory
computer.Mount{Guest: "/reference", Host: docs, ReadOnly: true}
```

A host directory is opened with `files.Dir`, which goes through Go's `os.Root` and cannot be left by `..` or by a
symbolic link. A filesystem of your own decides for itself what a name may reach.

## Tools as commands and as Python

A server named `wiki` with a tool `read_wiki_structure` that takes `repoName` is:

```
wiki --help                                          # the server's tools
wiki read-wiki-structure --help                      # one tool's arguments
wiki read-wiki-structure --repo-name wazero/wazero
wiki read-wiki-structure --json '{"repoName": "wazero/wazero"}'
```

```python
from tools import wiki
help(wiki.read_wiki_structure)
wiki.read_wiki_structure(repo_name="wazero/wazero")
```

A command prints a text result as it is and anything else as JSON, ready for `jq`. The Python module is the name
as Python spells it: a server named `my-wiki` is the command `my-wiki` and the module `my_wiki`.

A server given without a name, as a bare URL, has its tools grouped by the part of their name before the first
underscore: a tool `notes_addNote` is `notes add-note`. This suits one server that fronts several applications.

## More commands

The `extra` package holds commands for everyday data and document work. `cmd/computer` includes them; the library
does not, so an embedder passes `extra.Commands()` in `Config.Commands`. Each command reads and writes files only
inside the mounts, and `NAME --help` lists its options.

```
magick photo.jpg -resize 800 small.png     # a subset of ImageMagick: also convert, identify, -crop, -rotate, -flip, -flop
chart bar --x month --y north,south sales.csv --out sales.png    # or line, pie, scatter; SVG or PNG
pdftotext -f 1 -l 3 report.pdf -           # a subset of poppler's: text of pages 1 to 3 on standard output
pdftoppm -png -r 100 report.pdf page       # writes page-1.png, page-2.png, ... (also -jpeg, -f, -l)
```


## What it is made of

| Part | What it does |
|---|---|
| [gbash](https://github.com/Lakeshore-Labs/gbash), our fork of [ewhauser/gbash](https://github.com/ewhauser/gbash) | The shell and its commands, in Go, with `jq`, `yq`, `awk`, `sqlite3`, `html-to-markdown` |
| [wazero](https://github.com/wazero/wazero) | Runs CPython as WebAssembly |
| [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) | Talks to the MCP servers |
| `wasm/` | Runs a WASI program like a command: memory cap, deadline, and directories that are afero filesystems |
| `files/` | A host directory as an afero filesystem that cannot be left |
| `extra/` | Commands the shell lacks: `magick`, `convert`, `identify` (a subset of ImageMagick), `chart`, `pdftotext` and `pdftoppm` (a subset of poppler's, PDFium as WebAssembly) |
| `tools/` | Turns MCP tools into commands and a Python package |
| `computer.go` | Assembles the above |
| `cmd/computer` | The command-line program and the MCP server mode |

## Python

`python3` needs a CPython built for WASI, passed with `-python` and `-python-lib`. Without it the shell, SQLite and
the tool commands still work.

```
gh release download python-3.15.2-3 --repo Lakeshore-Labs/computer --pattern '*.tar.gz'
mkdir python-wasi && tar -xzf python-3.15.2-wasi.tar.gz -C python-wasi
./computer -mount ./work:/home/user -python python-wasi/python.wasm -python-lib python-wasi/lib
```

The build is made by a workflow in this repository from [`python/build.sh`](python/build.sh). See
[`python/README.md`](python/README.md) for what is in it and how to build it yourself.

## The boundary

- **Files.** The shell and programs see only the mounted folders. Programs reach them through Go's `os.Root`.
  Neither can create a symbolic link. The shell may write only inside writable mounts.
- **Network.** None, unless the embedder passes gbash's network options, which allow `curl` to listed URL prefixes.
- **Python and SQLite** run as WebAssembly with their own memory. Python has a memory cap for each run, and
  a limit on how many run at once (`-python-runs`, 8 by default), and stops at the request's deadline. The shell's `sqlite3` also runs as
  WebAssembly, and we set no memory cap on it.
- **The shell and its Go commands run in the host process.** gbash limits them, but it does not claim to be a
  hardened sandbox. Put the process in a container with CPU and memory limits if runaway scripts matter.
- **MCP tool names and descriptions** are treated as untrusted text when commands and Python are generated.

## Known limits

- Stopping a WebAssembly run at its deadline makes Python three to four times slower. `wasm.Program.Unstoppable`
  trades that away for trusted scripts.
- In our Python build `asyncio` and thread timeouts do not work, and there is no numpy or pandas.
- A shell command reads a file of at most 256 MiB, and a script runs at most 1,000,000 commands and as many loop
  iterations; a command's captured output is cut at gbash's default of 1 MiB. A larger file has to be handled in
  Python. An embedder changes these with `gbash.WithLimitOverrides` in `Config.Options`.
- Developed on macOS; the tests run on Linux in CI.

## Tests

```
go test .
```

The tests that need Python are skipped unless `PYTHON_WASM`, `PYTHON_LIB` and `PYTHON_HOST_MODULE` are set.

## License

Apache License 2.0. See [LICENSE](LICENSE).
