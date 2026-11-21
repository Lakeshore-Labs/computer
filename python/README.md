# CPython for WASI

`python3` in the computer is CPython 3.15.2 built for `wasm32-wasip1`, with zlib and SQLite linked in and one extra
built-in module, `_host`, through which a script calls MCP tools.

## Download it

Each build is made by the `python` workflow in this repository from `build.sh` and attached to a release:

    gh release download python-3.15.2-1 --repo Lakeshore-Labs/computer --pattern '*.tar.gz*'
    shasum -a 256 -c python-3.15.2-wasi.tar.gz.sha256
    mkdir python-wasi && tar -xzf python-3.15.2-wasi.tar.gz -C python-wasi

    computer -mount ./work:/home/user -python python-wasi/python.wasm -python-lib python-wasi/lib

The archive is about 16 MB. It holds `python.wasm` and `lib/python3.15/`, the standard library without its tests,
`ensurepip`, `idlelib`, `tkinter` and `turtledemo`.

## Build it

From an empty working directory, on Linux or macOS:

    mkdir work && cd work && sh /path/to/python/build.sh

It writes `downloads/`, `src/`, `build/` and `dist/` there, and takes a few minutes. It needs `curl`, `tar`,
`unzip`, `make` and a C compiler; on Debian or Ubuntu, `apt-get install build-essential ca-certificates curl unzip`.
It downloads wasi-sdk 33, CPython 3.15.2, zlib 1.3.1 and SQLite 3.46.1, each pinned to a version and checked
against a SHA-256 written in the script.

## What differs from a stock CPython build

- **`_host`.** `_host.c` is registered as a static module. `_host.call(message: bytes) -> bytes` sends one JSON
  message to the host and returns its JSON reply. The module imports `call` and `read` from the WebAssembly import
  module named `computer`, so the host must provide that module; the `wasm` package of this repository does.
- **zlib and SQLite are linked in statically**, since a WASI build cannot load extension modules. SQLite is built
  without write-ahead logging, shared cache and extension loading, and with FTS5 and the math functions.
- **`clock_nanosleep` is disabled.** CPython would otherwise sleep until an absolute time, which wazero does not
  support, and `time.sleep` would fail. Two configure values are needed, because configure repeats the check with
  `-lrt`.
- **No WASI runtime is needed to build.** CPython's configure asks for one, to run its own tests; the script names
  one that is never run.

## Known gaps

`asyncio.run` and lock or event timeouts do not work in this build. There are no third-party packages; a
pure-Python package can be added by putting it under `lib/python3.15/site-packages`.
