# The embedded Python

`python3` in the computer is [wasi-python](https://github.com/ClayGendron/wasi_python) 0.1.6, by Clay Gendron,
under the Apache License 2.0: CPython 3.15.2 for `wasm32-wasip1`, linked into one static binary with numpy, pandas,
scipy, scikit-learn, statsmodels, matplotlib, Pillow, lxml and the other compiled packages, and a zip of the standard
library and every package's Python code. Its [package list](https://github.com/ClayGendron/wasi_python/blob/dev/docs/reference/packages.md)
and [limitations](https://github.com/ClayGendron/wasi_python/blob/dev/docs/reference/limitations.md) say what works.

The files here are from the release's `wasi-python-0.1.6-wasm32-wasip1.tar.xz`, checked against its `SHA256SUMS`:

| file | what it is |
| --- | --- |
| `python.wasm.gz` | the release's `python.wasm`, the plain reactor, gzipped (`gzip -9 -n`), since Go reads gzip and not xz |
| `python-stdlib.zip` | the release's standard library and packages, as it is |
| `SHA256SUMS`, `MANIFEST`, `LICENSES/` | the release's checksums, pinned sources and licence notices |

The release's `python-snapshot.wasm` is not used. `computer.EmbeddedPython` unpacks `python.wasm`, puts the zip alone
in `lib/` as `python315.zip`, and extracts the zip's `matplotlib/mpl-data.zip` into `mpl-data/`. The computer mounts
`lib/` at `/usr/local/lib` and `mpl-data/` at `/usr/local/lib/python315.zip/matplotlib/mpl-data`, as wasi-python's
[embedding guide](https://github.com/ClayGendron/wasi_python/blob/dev/docs/how-to/embed-from-other-languages.md)
asks, answers its one import, `host::call`, with the computer's tools (`wasm.Program.Host`), and calls its
`_initialize` and then `run`. Every run starts with a short program (`pythonStartCode` in `computer.go`) that moves to
the shell's working directory and puts the generated `tools` package on the path, since wasi-python runs Python in
isolated mode, and then runs the program's own arguments.

## Embed a new release

    gh release download vX.Y.Z --repo ClayGendron/wasi_python --pattern '*-wasm32-wasip1.tar.xz' --pattern SHA256SUMS
    shasum -a 256 -c --ignore-missing SHA256SUMS
    mkdir release && tar xJf wasi-python-X.Y.Z-wasm32-wasip1.tar.xz -C release --strip-components=1
    (cd release && shasum -a 256 -c SHA256SUMS)
    gzip -9 -n -c release/python.wasm > python/python.wasm.gz
    cp release/python-stdlib.zip release/SHA256SUMS release/MANIFEST python/
    rm -r python/LICENSES && cp -R release/LICENSES python/

Then run `go test ./...`. The unpacked directory is named for `SHA256SUMS`, so a new release unpacks afresh.
