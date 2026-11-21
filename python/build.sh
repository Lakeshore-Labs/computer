#!/bin/sh
# Builds CPython 3.15.2 for wasm32-wasip1 with wasi-sdk 33, zlib and SQLite linked in statically, and the
# built-in module _host. Run it from an empty working directory; it writes downloads/, src/, build/ and dist/
# there. Set JOBS to change the parallelism. See README.md for the required host packages.
set -eu

HERE=$(cd "$(dirname "$0")" && pwd)
WORK=$(pwd)
JOBS=${JOBS:-$( (nproc || sysctl -n hw.ncpu || echo 4) 2>/dev/null )}

PYTHON_VERSION=3.15.2
PYTHON_SHA256=f74e1db0eabd651cd01e4e2a00201bd41866abe74869ab716f9bf33cbbaa0943
ZLIB_SHA256=9a93b2b7dfdac77ceba5a558a580e74667dd6fede4585b91eefb60f03b72df23
SQLITE_SHA256=77823cb110929c2bcb0f5d48e4833b5c59a8a6e40cdea3936b99e199dbbe5784
WASI_SDK_URL=https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-33

case "$(uname -s)-$(uname -m)" in
Linux-x86_64)  SDK=x86_64-linux; WASI_SDK_SHA256=0ba8b5bfaeb2adf3f29bab5841d76cf5318ab8e1642ea195f88baba1abd47bce ;;
Linux-aarch64|Linux-arm64) SDK=arm64-linux; WASI_SDK_SHA256=4f98ee738c7abb45c81a94d1461fc53cc569d1cd01498951c8184d841a027844 ;;
Darwin-arm64)  SDK=arm64-macos; WASI_SDK_SHA256=85c997a2665ead91673b5bb88b7d0df3fc8900df3bfa244f720d478187bbdc78 ;;
Darwin-x86_64) SDK=x86_64-macos; WASI_SDK_SHA256=18f3f201ba9734e6a4455b0b6410690395a55e9ffa9f6f5066f66083a94b93b3 ;;
*) echo "unsupported platform $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

sha256() { if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }

# fetch URL FILE SHA256: download once into downloads/ and always verify.
fetch() {
	mkdir -p "$WORK/downloads"
	[ -f "$WORK/downloads/$2" ] || curl -fsSL --retry 3 -o "$WORK/downloads/$2" "$1"
	[ "$(sha256 "$WORK/downloads/$2")" = "$3" ] || { echo "checksum mismatch for $2" >&2; exit 1; }
}

fetch "$WASI_SDK_URL/wasi-sdk-33.0-$SDK.tar.gz" wasi-sdk.tar.gz "$WASI_SDK_SHA256"
fetch "https://www.python.org/ftp/python/$PYTHON_VERSION/Python-$PYTHON_VERSION.tgz" python.tgz "$PYTHON_SHA256"
fetch https://zlib.net/fossils/zlib-1.3.1.tar.gz zlib.tar.gz "$ZLIB_SHA256"
fetch https://www.sqlite.org/2024/sqlite-amalgamation-3460100.zip sqlite.zip "$SQLITE_SHA256"

mkdir -p "$WORK/src" "$WORK/build"
[ -d "$WORK/src/wasi-sdk" ] || { mkdir "$WORK/src/wasi-sdk.tmp" && tar xzf "$WORK/downloads/wasi-sdk.tar.gz" -C "$WORK/src/wasi-sdk.tmp" --strip-components=1 && mv "$WORK/src/wasi-sdk.tmp" "$WORK/src/wasi-sdk"; }
[ -d "$WORK/src/Python-$PYTHON_VERSION" ] || tar xzf "$WORK/downloads/python.tgz" -C "$WORK/src"
[ -d "$WORK/src/zlib-1.3.1" ] || tar xzf "$WORK/downloads/zlib.tar.gz" -C "$WORK/src"
[ -d "$WORK/src/sqlite-amalgamation-3460100" ] || unzip -q "$WORK/downloads/sqlite.zip" -d "$WORK/src"

S=$WORK/src/wasi-sdk
SRC=$WORK/src/Python-$PYTHON_VERSION
DEPS=$WORK/build/deps
WASICC="$S/bin/clang --target=wasm32-wasip1 --sysroot=$S/share/wasi-sysroot"

# 1. The host Python that the cross build runs for code generation (freezing modules, sysconfigdata).
BUILD_TRIPLE=$("$SRC/config.guess")
HOSTPY_DIR=$WORK/build/host
# The binary is python.exe on macOS, where a `python` target or test would also match the Python/ directory.
if [ ! -f "$HOSTPY_DIR/python" ] && [ ! -f "$HOSTPY_DIR/python.exe" ]; then
	mkdir -p "$HOSTPY_DIR"
	(cd "$HOSTPY_DIR" && "$SRC/configure" -q && make -j "$JOBS")
fi
HOSTPY=$HOSTPY_DIR/python
[ -f "$HOSTPY" ] || HOSTPY=$HOSTPY_DIR/python.exe

# 2. zlib 1.3.1, static.
mkdir -p "$DEPS"
if [ ! -f "$DEPS/lib/libz.a" ]; then
	rm -rf "$WORK/build/zlib" && cp -R "$WORK/src/zlib-1.3.1" "$WORK/build/zlib"
	(cd "$WORK/build/zlib" &&
		CC="$WASICC" AR="$S/bin/llvm-ar" RANLIB="$S/bin/llvm-ranlib" CFLAGS=-O2 ./configure --static --prefix="$DEPS" &&
		make -j "$JOBS" libz.a AR="$S/bin/llvm-ar" ARFLAGS=rc && make install)
fi

# 3. SQLite 3.46.1 amalgamation, static. The options were read from the compile-options table of the reference build.
if [ ! -f "$DEPS/lib/libsqlite3.a" ]; then
	mkdir -p "$WORK/build/sqlite" "$DEPS/lib" "$DEPS/include"
	(cd "$WORK/build/sqlite" &&
		$WASICC -O2 -DSQLITE_THREADSAFE=0 -DSQLITE_OMIT_LOAD_EXTENSION -DSQLITE_OMIT_SHARED_CACHE -DSQLITE_OMIT_WAL \
			-DSQLITE_ENABLE_FTS5 -DSQLITE_ENABLE_MATH_FUNCTIONS \
			-c "$WORK/src/sqlite-amalgamation-3460100/sqlite3.c" -o sqlite3.o &&
		"$S/bin/llvm-ar" rcs "$DEPS/lib/libsqlite3.a" sqlite3.o)
	cp "$WORK/src/sqlite-amalgamation-3460100/sqlite3.h" "$WORK/src/sqlite-amalgamation-3460100/sqlite3ext.h" "$DEPS/include/"
fi

# 4. CPython for wasm32-wasip1. configure insists that HOSTRUNNER is set but only records it (in the Makefile and
# sysconfigdata) for `make test`; the build never runs python.wasm, so wasmtime need not be installed.
cp "$HERE/_host.c" "$SRC/Modules/_host.c"
export AR=$S/bin/llvm-ar CC=$S/bin/clang CPP=$S/bin/clang-cpp CXX=$S/bin/clang++ RANLIB=$S/bin/ranlib WASI_SDK_PATH=$S
export HOSTRUNNER=wasmtime CONFIG_SITE=$SRC/Platforms/WASI/config.site-wasm32-wasi PATH=$S/bin:$PATH
export PKG_CONFIG_LIBDIR=$S/share/wasi-sysroot/lib/pkgconfig:$S/share/wasi-sysroot/share/pkgconfig PKG_CONFIG_PATH= PKG_CONFIG_SYSROOT_DIR=$S/share/wasi-sysroot
B=$WORK/build/wasm32-wasip1
rm -rf "$B"; mkdir -p "$B/Modules"; cd "$B"
# The two ac_cv_ values disable clock_nanosleep: configure retries the check with -lrt, so both are needed.
"$SRC/configure" --host=wasm32-wasip1 --build="$BUILD_TRIPLE" --with-build-python="$HOSTPY" \
	ac_cv_func_clock_nanosleep=no ac_cv_lib_rt_clock_nanosleep=no \
	ZLIB_CFLAGS="-I$DEPS/include" "ZLIB_LIBS=-L$DEPS/lib -lz" \
	LIBSQLITE3_CFLAGS="-I$DEPS/include" "LIBSQLITE3_LIBS=-L$DEPS/lib -lsqlite3"
if grep -q '^#define HAVE_CLOCK_NANOSLEEP' pyconfig.h; then echo "HAVE_CLOCK_NANOSLEEP is defined" >&2; exit 1; fi
# Register _host as a static module. Touching Setup.local makes make regenerate the Makefile and config.c,
# which takes a first run; the second run builds with the regenerated files.
printf '*static*\n_host _host.c\n' > Modules/Setup.local
touch Modules/Setup.local
make -j "$JOBS" all || true
make -j "$JOBS" all

# 5. dist/: the wasm and the trimmed standard library.
D=$WORK/dist
rm -rf "$D"; mkdir -p "$D/lib/python3.15/lib-dynload"
cp "$B/python.wasm" "$D/python.wasm"
(cd "$SRC/Lib" && tar cf - --exclude __pycache__ --exclude '*.pyc' --exclude test --exclude ensurepip \
	--exclude idlelib --exclude tkinter --exclude turtledemo .) | (cd "$D/lib/python3.15" && tar xf -)
cp "$B"/build/lib.wasi-wasm32-3.15/_sysconfigdata__wasi_wasm32-wasi.py "$D/lib/python3.15/"
ls -l "$D/python.wasm"
