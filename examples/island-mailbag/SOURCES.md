# Sources and attribution

## The book (not copied here)

*Four moons of Jupiter*, the pocket astronomy EPUB published in the Lakeshore-Labs/computer repository at commit
`71684674a05273f6c689b5d3c78d8e8f19d8191c`, file `examples/moon-almanac/sample/four-moons.epub`
(121,382 bytes, SHA-256 `db45721e5c875bc1be2b4b0870132c70d04d2e0aa6e29b48729ee771268b8ee5`). This recipe reads it from your
checkout, read-only, and never modifies, re-packs or chunks it. The commit is on the repository's `showcase/moon-almanac`
branch; it is not in the `main` tree at `ea7bf2356c67`, so check out that commit (or that branch) to have the file.

The book's own credits: photograph PIA00601, NASA/JPL/DLR; planetary satellite physical parameters, NASA/JPL Solar System
Dynamics; entities from Wikidata. Nothing here adds to the book.

Made up for this exhibit: Gull Island Library, the Ferry Landing Reading Room, the reading card and its two times.

## Computer

Lakeshore-Labs/computer, revision `ea7bf2356c679b2b2ebddf0498be4616da22ec83`. Every guest step in the recipe is one
invocation of the Computer binary you pass (`-python` and `-python-lib` from the checkout's `bin/python-wasi-2`, CPython for
WebAssembly). The receipts and the two CARs were made with a binary built from that revision. No Computer code is changed or
copied here; `recipe/guest/*` are small scripts that run inside its guests.

## JavaScript (Store A, the writer; `recipe/node/package-lock.json` pins every one)

| package | version | license |
|---|---|---|
| @ipld/car | 6.2.0 | Apache-2.0 OR MIT |
| @ipld/dag-cbor | 11.0.0 | Apache-2.0 OR MIT |
| blockstore-fs | 4.0.1 | Apache-2.0 OR MIT |
| multiformats | 14.0.6 | Apache-2.0 OR MIT |

Transitive, installed with them: @nodelib/fs.scandir 2.1.5 (MIT), @nodelib/fs.stat 2.0.5 (MIT), @nodelib/fs.walk 1.2.8 (MIT),
abort-error 1.0.2 (Apache-2.0 OR MIT), braces 3.0.3 (MIT), cborg 7.0.0 (Apache-2.0), fast-glob 3.3.3 (MIT), fastq 1.20.3 (ISC),
fill-range 7.1.1 (MIT), glob-parent 5.1.2 (ISC), interface-blockstore 7.0.1 (Apache-2.0 OR MIT), interface-store 8.0.0
(Apache-2.0 OR MIT), is-extglob 2.1.1 (MIT), is-glob 4.0.3 (MIT), is-number 7.0.0 (MIT), it-batch 3.0.11 (Apache-2.0 OR MIT),
it-glob 3.0.6 (Apache-2.0 OR MIT), it-map 3.1.6 (Apache-2.0 OR MIT), it-parallel-batch 3.0.11 (Apache-2.0 OR MIT),
it-peekable 3.0.10 (Apache-2.0 OR MIT), merge2 1.4.1 (MIT), micromatch 4.0.8 (MIT), picomatch 2.3.2 (MIT),
queue-microtask 1.2.3 (MIT), race-signal 2.0.2 (Apache-2.0 OR MIT), reusify 1.1.0 (MIT), run-parallel 1.2.0 (MIT),
steno 4.0.2 (MIT), to-regex-range 5.0.1 (MIT), varint 6.0.0 (MIT).

## Python (Store B, the independent reader, and `verify.py`; `recipe/requirements.txt` pins every one)

| package | version | license (as stated in its metadata) |
|---|---|---|
| carbox | 0.3 | MIT |
| libipld | 4.0.0 | MIT |
| cbor2 | 5.9.0 | MIT |
| dag-cbor | 0.3.3 | not stated |
| multiformats | 0.3.1.post4 | not stated |
| multiformats-config | 0.3.1 | not stated |
| bases | 0.3.0 | not stated |
| typing-validation | 2.3.0 | LGPL-3.0-or-later |
| typing-extensions | 4.16.0 | PSF-2.0 |

Standard library: hashlib, sqlite3, base64, zipfile. `typing-validation` (needed by the Python `multiformats` that `carbox`
pulls in) is LGPL-3.0-or-later; it is installed into your own virtual environment and nothing here ships or links it.

## Specifications

- CARv1: https://ipld.io/specs/transport/car/carv1/
- CID: https://github.com/multiformats/cid
- multihash: https://github.com/multiformats/multihash
- DAG-CBOR: https://ipld.io/specs/codecs/dag-cbor/spec/
- EPUB 3: https://www.w3.org/TR/epub-33/

## What this recipe's own files are

`recipe/`, `index.html`, `car/` and `receipts/` were written for this exhibit. No license file is included; add the one you
intend to publish under before you publish (see README, Limits).
