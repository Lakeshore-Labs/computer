# computer

A sandboxed computer for an AI agent, as a Go library and one binary. No Node, no container, no cgo.

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

A server that needs a bearer token reads it from the environment: `COMPUTER_MCP_TOKEN_WIKI` for a server named
`wiki`. A server started as a program runs on the host, outside the sandbox, like any other program you start.

For Claude Code:

```
claude mcp add computer -- /path/to/computer -mount /path/to/work:/home/user -mcp wiki=https://mcp.deepwiki.com/mcp mcp
```

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

A command prints a text result as it is and anything else as JSON, ready for `jq`.

A server given without a name, as a bare URL, has its tools grouped by the part of their name before the first
underscore: a tool `notes_addNote` is `notes add-note`. This suits one server that fronts several applications.

## More commands

The `extra` package holds commands for everyday data and document work. `cmd/computer` includes them; the library
does not, so an embedder passes `extra.Commands()` in `Config.Commands`. Each command reads and writes files only
inside the mounts, and `NAME --help` lists its options.

```
image resize photo.jpg small.png --width 800      # also info, convert, thumbnail, crop and rotate
chart bar --x month --y north,south sales.csv --out sales.png    # or line, pie, scatter; SVG or PNG
```


## What it is made of

| Part | What it does |
|---|---|
| [gbash](https://github.com/ewhauser/gbash) | The shell and its commands, in Go, with `jq`, `yq`, `awk`, `sqlite3`, `html-to-markdown` |
| [wazero](https://github.com/wazero/wazero) | Runs CPython as WebAssembly |
| [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) | Talks to the MCP servers |
| `wasm/` | Runs a WASI program like a command: memory cap, deadline, and directories that are afero filesystems |
| `files/` | A host directory as an afero filesystem that cannot be left |
| `wasm/` | Runs a WASI program like a command: memory cap, deadline, and mounts it cannot leave |
| `extra/` | Commands the shell lacks: `image` and `chart`, one file each |
| `tools/` | Turns MCP tools into commands and a Python package |
| `agent/` | The command `agent`: runs the agent and answers its requests for the shell, files and the model |
| `computer.go` | Assembles the above |
| `cmd/computer` | The command-line program and the MCP server mode |

## Python

`python3` needs a CPython built for WASI, passed with `-python` and `-python-lib`. Without it the shell, SQLite and
the tool commands still work.

```
gh release download python-3.15.2-2 --repo Lakeshore-Labs/computer --pattern '*.tar.gz'
mkdir python-wasi && tar -xzf python-3.15.2-wasi.tar.gz -C python-wasi
./computer -mount ./work:/home/user -python python-wasi/python.wasm -python-lib python-wasi/lib
```

The build is made by a workflow in this repository from [`python/build.sh`](python/build.sh). See
[`python/README.md`](python/README.md) for what is in it and how to build it yourself.

## An agent in the computer

`agent` is an agent as a command: [Pi Durable](https://github.com/earendil-works/pi), unmodified, running on QuickJS
as one more WebAssembly program. Its tools are the computer's shell and files, so it uses MCP tools, Python and
SQLite the way a person at the shell would.

```
gh release download agent-1.5.2-5 --repo Lakeshore-Labs/computer --pattern agent.tar.gz
mkdir agent-wasi && tar -xzf agent.tar.gz -C agent-wasi
export COMPUTER_MODEL_KEY=...
./computer -mount ./work:/home/user -mcp wiki=https://mcp.deepwiki.com/mcp \
    -agent agent-wasi -agent-engine agent-wasi/qjs-wasi.wasm \
    -model-url https://api.openai.com/v1 -model gpt-6-sol \
    -c 'agent "What is wazero? Ask the wiki, and answer in two sentences."'
```

- It prints its answer on standard output and shows each step it takes on standard error, so a person watches the
  run and `agent "..." > answer.txt` keeps the answer alone. `-q` shows no steps. `--json` prints the whole run as
  JSON lines for a program, as Pi Durable's own JSON mode does: the transcript entries as they are added and the
  live state, then `{"answer": ...}` or `{"unanswered": reason}`.
- The conversation is stored at every step, in a directory outside every mount (`-agent-state`, by default one
  under your cache directory named for the first mount). The next `agent` continues it, and a run a crash cut off
  goes on where it stopped. No command and none of the agent's tools can reach that directory, so the model cannot
  rewrite what the next run is told.
- One agent runs at a time over a conversation; a second, such as one the agent starts itself, is refused.
- The model is any OpenAI-compatible chat-completions address. The command makes the request; the agent never
  holds the key or the address.
- Stopping the command asks the agent to abort its run, which it records, so the next message starts a new one. An
  agent that does not stop is stopped where it stands.
- The bundle is compiled to the engine's bytecode once and loaded from there, so a run starts in about 0.15 s.

One `agent` is one message and its answer. A message cannot yet be given to a run that is under way, and nothing
asks a person to approve a step.

The sources are in [`agent/js`](agent/js); `npm ci && npm run build` there makes `dist/agent.js`. The environment
Pi Durable's tools work in is checked with Pi Durable's own conformance suite: 14 of its 22 cases pass, and the
other 8 are for reporting file changes, which this environment does not do.

## The boundary

- **Files.** The shell and programs see only the mounted folders. Programs reach them through Go's `os.Root`.
  Neither can create a symbolic link. The shell may write only inside writable mounts.
- **Network.** None, unless the embedder passes gbash's network options, which allow `curl` to listed URL prefixes.
- **Python and SQLite** run as WebAssembly with their own memory. Python has a memory cap and stops at the
  request's deadline.
- **The shell and its Go commands run in the host process.** gbash limits them, but it does not claim to be a
  hardened sandbox. Put the process in a container with CPU and memory limits if runaway scripts matter.
- **MCP tool names and descriptions** are treated as untrusted text when commands and Python are generated.

## Known limits

- Stopping a WebAssembly run at its deadline makes Python three to four times slower. `wasm.Program.Unstoppable`
  trades that away for trusted scripts.
- In our Python build `asyncio` and thread timeouts do not work, and there is no numpy or pandas.
- Tested on macOS and Linux. It compiles for Windows and has not been run there.

## Tests

```
go test .
```

The tests that need Python are skipped unless `PYTHON_WASM`, `PYTHON_LIB` and `PYTHON_HOST_MODULE` are set.

## License

Apache License 2.0. See [LICENSE](LICENSE).
