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

# an MCP server's tools as commands (bearer token from COMPUTER_MCP_TOKEN)
./computer -mount ./work:/home/user -mcp https://example.com/mcp -c 'stores list-stores --help'

# several servers: name=URL makes one command per server
./computer -mount ./work:/home/user -mcp notes=https://a.example/mcp -mcp crm=https://b.example/mcp

# as an MCP server for an agent: its tools are bash, read, edit and write
./computer -mount ./work:/home/user -mcp https://example.com/mcp mcp
```

For Claude Code:

```
claude mcp add computer -- /path/to/computer -mount /path/to/work:/home/user -mcp https://example.com/mcp mcp
```

From Go:

```go
c, err := computer.New(computer.Config{
    Mounts: []computer.Mount{{Guest: "/home/user", Host: dir}},
    Tools:  []tools.Source{{Session: session}}, // an MCP client session
    Python: python,                             // from computer.LoadPython, shared by every computer
})
defer c.Close()
res, err := c.Exec(ctx, &gbash.ExecutionRequest{Script: "stores list-stores | jq length"})
```

## Tools as commands and as Python

A tool named `stores_listStores` with an argument `minId` is:

```
stores --help
stores list-stores --help
stores list-stores --min-id 2
stores list-stores --json '{"minId": 2}'
```

```python
from tools import stores
stores.list_stores(min_id=2)
```

A server given as `name=URL` is one command and one module of that name. A server given as a bare URL has its tools
grouped by the part of their name before the first underscore.

## What it is made of

| Part | What it does |
|---|---|
| [gbash](https://github.com/ewhauser/gbash) | The shell and its commands, in Go, with `jq`, `yq`, `awk`, `sqlite3`, `html-to-markdown` |
| [wazero](https://github.com/wazero/wazero) | Runs CPython as WebAssembly |
| [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) | Talks to the MCP servers |
| `wasm/` | Runs a WASI program like a command: memory cap, deadline, and mounts it cannot leave |
| `tools/` | Turns MCP tools into commands and a Python package |
| `computer.go` | Assembles the above |
| `cmd/computer` | The command-line program and the MCP server mode |

## Python

`python3` needs a CPython built for WASI, passed with `-python` and `-python-lib`. The build must include a small
module, `_host`, through which scripts call tools. It is not in this repository. Without it the shell, SQLite and
the tool commands still work.

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
- A mount is a host folder. In-memory and other filesystems are not supported yet.
- Tested on macOS only.

## Tests

```
go test .
```

The tests that need Python are skipped unless `PYTHON_WASM`, `PYTHON_LIB` and `PYTHON_HOST_MODULE` are set.
