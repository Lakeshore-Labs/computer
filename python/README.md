# CPython for WASI

`python3` in the computer is CPython 3.15.2 built for `wasm32-wasip1`, with zlib and SQLite linked in and one extra
built-in module, `_host`, through which a script calls MCP tools.

## Download it

Each build is made by the `python` workflow in this repository from `build.sh` and attached to a release:

    gh release download python-3.15.2-2 --repo Lakeshore-Labs/computer --pattern '*.tar.gz*'
    shasum -a 256 -c python-3.15.2-wasi.tar.gz.sha256
    mkdir python-wasi && tar -xzf python-3.15.2-wasi.tar.gz -C python-wasi

    computer -mount ./work:/home/user -python python-wasi/python.wasm -python-lib python-wasi/lib

The archive is about 30 MB; the library in it is compiled ahead of time, so an import does not compile its source
on every run. It holds `python.wasm` and `lib/python3.15/`, the standard library without its tests, `ensurepip`,
`idlelib`, `tkinter` and `turtledemo`, and the packages listed below in `site-packages`.

## Build it

From an empty working directory, on Linux or macOS:

    mkdir work && cd work && sh /path/to/python/build.sh

It writes `downloads/`, `src/`, `build/` and `dist/` there, and takes a few minutes. It needs `curl`, `tar`,
`unzip`, `make` and a C compiler; on Debian or Ubuntu, `apt-get install build-essential ca-certificates curl unzip`.
It downloads wasi-sdk 33, CPython 3.15.2, zlib 1.3.1, SQLite 3.46.1 and the packages in `requirements.txt`, each
pinned to a version and checked against a SHA-256 written in the script or in that file.

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

## Bundled packages

`requirements.txt` pins each package to a PyPI file and its SHA-256. `build.sh` downloads the file, checks the hash
and unpacks it into `lib/python3.15/site-packages` without pip, so no package code runs during the build. The
licence files stay in each package's `*.dist-info`.

| Package | Version | For |
| --- | --- | --- |
| openpyxl (et_xmlfile) | 3.1.5 (2.0.0) | read and write `.xlsx` |
| XlsxWriter | 3.3.3 | write `.xlsx` with formats and charts |
| pypdf | 7.1.1 | read text from, merge and split PDFs |
| fpdf2 (fonttools, defusedxml) | 2.8.7 (4.68.0, 0.7.1) | create PDFs; no images, because Pillow is absent |
| mammoth (cobble) | 1.13.0 (0.1.4) | read `.docx` as HTML or text |
| PyYAML | 6.0.4 | YAML, without libyaml |
| Jinja2 (MarkupSafe) | 3.1.6 (3.0.4) | templates |
| python-dateutil (tzdata) | 4.0.0 (2026.6) | date parsing and arithmetic |
| tabulate | 0.10.0 | text tables |
| beautifulsoup4 (soupsieve, typing_extensions) | 4.15.0 (4.1.3, 4.16.0) | parse HTML with `html.parser` |
| markdownify | 1.2.4 | HTML to Markdown |
| Markdown | 3.11.3 | Markdown to HTML |
| tomli_w | 1.2.0 | write TOML; `tomllib` reads it |

The rule is pure Python only. The sandbox cannot load C extension modules, has no network and no `ssl`, so a
package with a compiled part (lxml, Pillow, numpy, pandas) cannot work, and neither can one that needs the network.
Check a new package by importing it and running a round trip in the computer before adding it. These were tried and
left out: python-docx and docx2python (import lxml), reportlab (imports Pillow at import time), fpdf2 2.8.8 and
later (import `urllib.request.HTTPSHandler`, which needs `ssl`), and xlrd (imports `mmap`, which is absent).
PyYAML and MarkupSafe publish no pure wheel, so the source archive is used and only its Python package is copied.
The library is compiled to bytecode ahead of time (`build.sh` step 7), so an import does not compile its source
again, but importing a large package is still slow, and the first import of `fpdf` takes about ten seconds.

## Known gaps

`asyncio.run` and lock or event timeouts do not work in this build. `fpdf2` prints a warning that Pillow is missing.
