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

## Flask

`site.zip` is Flask and what it needs beyond the release (which has Jinja2 and MarkupSafe), pure Python, from
PyPI's wheels, each with its license in its `.dist-info`: Flask 3.1.3, Werkzeug 3.1.9, click 8.3.3,
itsdangerous 2.2.0, blinker 1.9.0. `computer.EmbeddedPython` adds its files to `lib/python315.zip` as it unpacks
the release, where the release's own packages are, and a release with Flask in its zip has it imported in the
image every run starts from. To make it again:

    pip download --no-deps --only-binary :all: --implementation py --abi none --platform any -d wheels \
        Flask==3.1.3 Werkzeug==3.1.9 click==8.3.3 itsdangerous==2.2.0 blinker==1.9.0
    for w in wheels/*.whl; do unzip -q "$w" -d site; done
    (cd site && find . -type f | LC_ALL=C sort | zip -q -X -9 ../python/site.zip -@)
