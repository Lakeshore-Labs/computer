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
    Python: python,                                           // from computer.EmbeddedPython, shared by every computer
})
defer c.Close()
res, err := c.Exec(ctx, &gbash.ExecutionRequest{Script: "wiki read-wiki-structure --repo-name wazero/wazero | wc -l"})
```

## A terminal in the browser

`c.Prompt(ctx, term, workDir, interrupt, size)` is the shell `./computer` gives a person at a terminal: a line
editor with history and Tab completion over any stream, with the working directory and exported variables carried
from line to line, and Ctrl-C ending only the running command. Each prompt is a shell of its own: two on one
computer keep their own directories and variables. `computer.TerminalHandler(get)` serves it over a WebSocket;
`get` is yours, and returns the requester's computer and working directory, or an error (403 for
`fs.ErrPermission`, else a 500 that logs your error and does not show it). You keep authentication and which
computer is whose. A request that is not a WebSocket, or whose page is of another origin, is refused before `get`
is called; `computer.CheckOrigin(func(*http.Request) bool)` is another rule, for pages on a development server or
behind a proxy that changes the Host. A client that answers no ping for a minute, or stops reading, ends its
prompt and command. The React component is `@lakeshore-labs/computer-react` (`packages/react`):

```go
http.Handle("/computer/terminal", computer.TerminalHandler(func(r *http.Request) (*computer.Computer, string, error) {
    user, ok := signedIn(r) // yours
    if !ok {
        return nil, "", fs.ErrPermission
    }
    return computers.For(user), "/home/" + user, nil // yours, which you close
}))
```

```tsx
// package.json, installed with pnpm, which installs a folder of a git repository (npm does not):
// "@lakeshore-labs/computer-react": "github:Lakeshore-Labs/computer#<commit>&path:/packages/react"
import { Terminal } from "@lakeshore-labs/computer-react"
import "@lakeshore-labs/computer-react/terminal.css" // xterm.js's styles; or @import it in your CSS

<Terminal url="/computer/terminal" onCommandEnd={(code) => refreshFiles()} />
```

The package is TypeScript source, which your bundler compiles and your `tsc` checks: React 19 and its types are
yours. Importing the CSS from code needs your bundler's types for it, such as Vite's `vite/client` in
`compilerOptions.types` (TypeScript 6 otherwise reports TS2882); an `@import` in your CSS needs none. The terminal
fills its parent, follows a `dark` class on `<html>`, and offers to reconnect when the connection closes. A
command's end reaches `onCommandEnd` as OSC 133's `D` mark, which the prompt writes after each command. Its tests
run on Node 22.18 or later, which runs TypeScript.

## Mounts

A mount is a directory of the computer. The shell and every program are given the same one, so a file one writes
another reads. It is a host directory, or anything that implements [afero](https://github.com/spf13/afero)'s
filesystem interface: memory, a database, an object store.

```go
computer.Mount{Guest: "/home/user", Host: dir}                     // a host directory
computer.Mount{Guest: "/scratch", FS: afero.NewMemMapFs()}         // kept in memory
computer.Mount{Guest: "/reference", Host: docs, ReadOnly: true}
```

A mount may be inside another: `/home/user/files` in `/home/user`. Its folder must already be there in the outer
mount, as a mount point is. A path belongs to the nearest mount around it, so a read-only mount inside a writable
one stays read-only.

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
| `extra/` | `pdftotext` and `pdftoppm`, a subset of poppler's, PDFium as WebAssembly; images and charts are Python's (Pillow, matplotlib) |
| `tools/` | Turns MCP tools into commands and a Python package |
| `computer.go` | Assembles the above |
| `cmd/computer` | The command-line program and the MCP server mode |

## Python

`python3` is [wasi-python](https://github.com/ClayGendron/wasi_python) 0.1.6: CPython 3.15 for WASI with numpy,
pandas, scipy, scikit-learn, statsmodels, matplotlib, Pillow, lxml, python-docx, python-pptx and about forty more
packages. The module embeds it and unpacks it once into the user's cache directory:

```go
python, err := computer.EmbeddedPython(ctx, wasm.Program{MaxRuns: 8, CacheDir: cacheDir})
```

The first compile takes about 30 seconds, then under a second from `CacheDir`; it runs in the background, and only
the first `python3` waits. Every `python3` starts from one image of the interpreter with numpy, pandas and
matplotlib already imported, made once (about 4 to 8 seconds) and kept in `CacheDir`; each run is still its own
interpreter, with its own memory, arguments, environment and files, and `random` and `numpy.random` seeded afresh.
Its string hashes are not randomized: every run has the image's hash seed. See [`python/README.md`](python/README.md).

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
- Python has no threads, processes, sockets, `ctypes` or `mmap`, and numpy's default integer is 32-bit.
- wasi-python needs wazero's experimental exception handling: wazero is pinned to a main commit with the fix for
  wazero#2503, and wazero#2522, a use-after-free in that support, is open.
- A shell command reads a file of at most 256 MiB, and a script runs at most 1,000,000 commands and as many loop
  iterations; a command's captured output is cut at gbash's default of 1 MiB. A larger file has to be handled in
  Python. An embedder changes these with `gbash.WithLimitOverrides` in `Config.Options`.
- Developed on macOS; the tests run on Linux in CI.

## Tests

```
go test .
```

The tests use the embedded Python, unless `PYTHON_DIR` names an unpacked release.

## License

Apache License 2.0. See [LICENSE](LICENSE).
