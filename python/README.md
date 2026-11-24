# The embedded Python

[wasi-python](https://github.com/ClayGendron/wasi_python) 0.1.6 (Apache 2.0), from its release
`wasi-python-0.1.6-wasm32-wasip1.tar.xz`, checked against its `SHA256SUMS`: `python.wasm`, gzipped (Go reads gzip,
not xz), `python-stdlib.zip`, `SHA256SUMS`, `MANIFEST` and `LICENSES/`. `computer.EmbeddedPython` unpacks them into
`python.wasm`, `lib/python315.zip` and `mpl-data/` (matplotlib's data, from inside the zip), the layout
`computer.LoadPython` and `-python DIR` take.

To embed a new release:

    gh release download vX.Y.Z --repo ClayGendron/wasi_python --pattern '*-wasm32-wasip1.tar.xz'
    mkdir release && tar xJf wasi-python-*.tar.xz -C release --strip-components=1
    (cd release && shasum -a 256 -c SHA256SUMS)
    gzip -9 -n -c release/python.wasm > python/python.wasm.gz
    cp release/python-stdlib.zip release/SHA256SUMS release/MANIFEST python/
    rm -r python/LICENSES && cp -R release/LICENSES python/
