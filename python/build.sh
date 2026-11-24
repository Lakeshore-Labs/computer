#!/bin/sh
# Builds CPython 3.15.2 for wasm32-wasip1 with wasi-sdk 33, zlib and SQLite linked in statically, the built-in
# module _host, and Pillow 13.0.0 (with libjpeg 9f) and lxml 6.1.3 (with libxml2 2.15.2 and libxslt 1.1.45) as
# built-in modules, which python-docx and python-pptx need. Run it from an empty working directory; it writes downloads/, src/, build/ and dist/
# there. Set JOBS to change the parallelism. See README.md for the required host packages.
set -eu

HERE=$(cd "$(dirname "$0")" && pwd)
WORK=$(pwd)
JOBS=${JOBS:-$( (nproc || sysctl -n hw.ncpu || echo 4) 2>/dev/null )}

PYTHON_VERSION=3.15.2
PYTHON_SHA256=f74e1db0eabd651cd01e4e2a00201bd41866abe74869ab716f9bf33cbbaa0943
ZLIB_SHA256=9a93b2b7dfdac77ceba5a558a580e74667dd6fede4585b91eefb60f03b72df23
SQLITE_SHA256=0a9c8e34352dad46006aa82836d5383bff9dc6bc9ce5a60553c05aa30e6ad1f9
# IJG publishes no checksum for libjpeg; this one is of the file served by ijg.org over HTTPS. The libxml2 and
# libxslt checksums are the ones lxml's buildlibxml.py pins, and the Pillow and lxml ones are PyPI's.
JPEG_SHA256=04705c110cb2469caa79fb71fba3d7bf834914706e9641a4589485c1f832565b
LIBXML2_SHA256=c8b9bc81f8b590c33af8cc6c336dbff2f53409973588a351c95f1c621b13d09d
LIBXSLT_SHA256=9acfe68419c4d06a45c550321b3212762d92f41465062ca4ea19e632ee5d216e
PILLOW_SHA256=cdcd1ba258178f7356d74170c9a81ba4eab6a5d55748bc402a5a2a6fe6be3510
LXML_SHA256=45222d94ddd511536f3b2f7d9deae3b2339b4ce0f075f1ca25703b07cad9dd21
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
fetch https://www.sqlite.org/2026/sqlite-amalgamation-3540100.zip sqlite.zip "$SQLITE_SHA256"
fetch https://www.ijg.org/files/jpegsrc.v9f.tar.gz jpeg.tar.gz "$JPEG_SHA256"
fetch https://download.gnome.org/sources/libxml2/2.15/libxml2-2.15.2.tar.xz libxml2.tar.xz "$LIBXML2_SHA256"
fetch https://download.gnome.org/sources/libxslt/1.1/libxslt-1.1.45.tar.xz libxslt.tar.xz "$LIBXSLT_SHA256"
fetch https://files.pythonhosted.org/packages/ce/a4/9cc30690a394a8305e8be4c9036aa16d0dfddadd09079860b582f562496d/pillow-13.0.0.tar.gz pillow.tar.gz "$PILLOW_SHA256"
fetch https://files.pythonhosted.org/packages/23/ad/28ecd7cb894d172f3c9c80a075eeeb2017ac62e3632cee05a5f9493547eb/lxml-6.1.3.tar.gz lxml.tar.gz "$LXML_SHA256"

mkdir -p "$WORK/src" "$WORK/build"
[ -d "$WORK/src/wasi-sdk" ] || { mkdir "$WORK/src/wasi-sdk.tmp" && tar xzf "$WORK/downloads/wasi-sdk.tar.gz" -C "$WORK/src/wasi-sdk.tmp" --strip-components=1 && mv "$WORK/src/wasi-sdk.tmp" "$WORK/src/wasi-sdk"; }
[ -d "$WORK/src/Python-$PYTHON_VERSION" ] || tar xzf "$WORK/downloads/python.tgz" -C "$WORK/src"
[ -d "$WORK/src/zlib-1.3.1" ] || tar xzf "$WORK/downloads/zlib.tar.gz" -C "$WORK/src"
[ -d "$WORK/src/sqlite-amalgamation-3540100" ] || unzip -q "$WORK/downloads/sqlite.zip" -d "$WORK/src"
[ -d "$WORK/src/pillow-13.0.0" ] || tar xzf "$WORK/downloads/pillow.tar.gz" -C "$WORK/src"
[ -d "$WORK/src/lxml-6.1.3" ] || tar xzf "$WORK/downloads/lxml.tar.gz" -C "$WORK/src"

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

# 3. SQLite 3.54.1 amalgamation, static. The options were read from the compile-options table of the reference build.
# Before 3.51.0, a WASI build called a null fchmod whenever it created a temporary file, and trapped.
if [ ! -f "$DEPS/lib/libsqlite3.a" ]; then
	mkdir -p "$WORK/build/sqlite" "$DEPS/lib" "$DEPS/include"
	(cd "$WORK/build/sqlite" &&
		$WASICC -O2 -DSQLITE_THREADSAFE=0 -DSQLITE_OMIT_LOAD_EXTENSION -DSQLITE_OMIT_SHARED_CACHE -DSQLITE_OMIT_WAL \
			-DSQLITE_ENABLE_FTS5 -DSQLITE_ENABLE_MATH_FUNCTIONS \
			-c "$WORK/src/sqlite-amalgamation-3540100/sqlite3.c" -o sqlite3.o &&
		"$S/bin/llvm-ar" rcs "$DEPS/lib/libsqlite3.a" sqlite3.o)
	cp "$WORK/src/sqlite-amalgamation-3540100/sqlite3.h" "$WORK/src/sqlite-amalgamation-3540100/sqlite3ext.h" "$DEPS/include/"
fi

# 3b. libjpeg 9f, static, for Pillow. Only the library is built: its command-line programs are not wanted.
# configure's --host names the target for config.sub; the compiler is given its own target in CC.
autotools() { # autotools ARCHIVE DIRECTORY: unpack a fresh copy into build/, so that its timestamps stay as shipped
	rm -rf "$WORK/build/$2" && mkdir -p "$WORK/build/$2"
	tar xf "$WORK/downloads/$1" -C "$WORK/build/$2" --strip-components=1
	cd "$WORK/build/$2"
}
CROSS="--host=wasm32-unknown-wasi --disable-shared --enable-static --prefix=$DEPS"
if [ ! -f "$DEPS/lib/libjpeg.a" ]; then
	autotools jpeg.tar.gz jpeg
	CC="$WASICC" AR="$S/bin/llvm-ar" RANLIB="$S/bin/llvm-ranlib" CFLAGS=-O2 ./configure -q $CROSS
	make -j "$JOBS" libjpeg.la
	make install-libLTLIBRARIES install-includeHEADERS
	cp jconfig.h "$DEPS/include/"
fi

# 3c. libxml2 2.15.2 and libxslt 1.1.45, static, for lxml. WASI has no dup(), which libxml2 calls when it reads a
# file it opened itself; libxml2-wasi.patch makes it keep that descriptor instead. lxml needs libxslt's profiler.
if [ ! -f "$DEPS/lib/libxml2.a" ]; then
	autotools libxml2.tar.xz libxml2
	patch -p1 < "$HERE/libxml2-wasi.patch"
	CC="$WASICC" AR="$S/bin/llvm-ar" RANLIB="$S/bin/llvm-ranlib" CFLAGS=-O2 ./configure -q $CROSS --without-python \
		--without-threads --without-iconv --without-modules --without-zlib --without-debug --without-catalog \
		--without-readline --without-icu --without-http
	make -j "$JOBS" libxml2.la
	make install-libLTLIBRARIES install-data
fi
if [ ! -f "$DEPS/lib/libxslt.a" ]; then
	autotools libxslt.tar.xz libxslt
	CC="$WASICC" AR="$S/bin/llvm-ar" RANLIB="$S/bin/llvm-ranlib" CFLAGS=-O2 LIBXML_CFLAGS="-I$DEPS/include/libxml2" \
		LIBXML_LIBS="-L$DEPS/lib -lxml2" ./configure -q $CROSS --without-python --without-crypto --without-debugger \
		--without-plugins
	(cd libxslt && make -j "$JOBS" libxslt.la && make install-libLTLIBRARIES install-xsltincHEADERS install-nodist_xsltincHEADERS)
	(cd libexslt && make -j "$JOBS" libexslt.la && make install-libLTLIBRARIES install-exsltincHEADERS install-nodist_exsltincHEADERS)
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
# Pillow's and lxml's extension modules, compiled against this configured CPython into one archive each. WASI
# cannot load an extension, so they are built in. Pillow's JPEG codec uses setjmp, which WASI builds as
# WebAssembly exceptions; wazero takes only the exnref form of them, not the legacy one.
WASI_INCLUDES="-I$SRC/Include -I$B -I$DEPS/include"
PIL=$WORK/src/pillow-13.0.0/src
rm -rf "$WORK/build/pillow" && mkdir -p "$WORK/build/pillow"
for f in "$PIL"/_imaging.c "$PIL"/decode.c "$PIL"/encode.c "$PIL"/map.c "$PIL"/display.c "$PIL"/outline.c "$PIL"/path.c \
	"$PIL"/libImaging/*.c; do
	case $f in */decode.c|*/encode.c|*/Jpeg*) sjlj="-mllvm -wasm-enable-sjlj -mllvm -wasm-use-legacy-eh=false" ;; *) sjlj= ;; esac
	o=$(basename "$f" .c)
	$WASICC -O2 $sjlj $WASI_INCLUDES -I"$PIL/libImaging" -DHAVE_LIBJPEG -DHAVE_LIBZ '-DPILLOW_VERSION="13.0.0"' \
		-c "$f" -o "$WORK/build/pillow/$o.o"
done
"$S/bin/llvm-ar" rcs "$DEPS/lib/libpillow.a" "$WORK/build/pillow"/*.o
LX=$WORK/src/lxml-6.1.3/src/lxml
$WASICC -O2 $WASI_INCLUDES -I"$DEPS/include/libxml2" -I"$LX/includes" -I"$LX" -DWITHOUT_THREADING -DLIBXML_STATIC \
	-DLIBXSLT_STATIC -DLIBEXSLT_STATIC -DCYTHON_CLINE_IN_TRACEBACK=0 -DCYTHON_USE_SYS_MONITORING=0 \
	-c "$LX/etree.c" -o "$WORK/build/lxml-etree.o"
"$S/bin/llvm-ar" rcs "$DEPS/lib/liblxml.a" "$WORK/build/lxml-etree.o"
# Register _host, Pillow's _imaging and lxml's etree as static modules. A built-in module has a top-level name, so
# PIL/_imaging.py and lxml/etree.py, written in step 6, stand for them in their packages. Deleting config.c makes
# make regenerate it and the Makefile, which takes a first run; the second run builds with the regenerated files.
# Touching Setup.local is not enough: configure wrote config.c in the same second, and a make with one-second
# timestamps (macOS) then sees nothing to do.
cat > Modules/Setup.local <<EOF
*static*
_host _host.c
_imaging -L$DEPS/lib -lpillow -ljpeg -lz -lsetjmp
etree -L$DEPS/lib -llxml -lexslt -lxslt -lxml2
EOF
rm -f Modules/config.c
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

# 6. Pure-Python packages from requirements.txt, unpacked into site-packages. pip is not used: each archive is
# downloaded from its pinned URL, checked against its SHA-256 and unpacked, so no package code runs. A wheel is
# unpacked as it is. PyYAML and MarkupSafe publish no pure wheel, so their source archives are used and the
# package directories under lib/ or src/ are copied; both fall back to Python when their C speedups are absent.
SP=$D/lib/python3.15/site-packages
mkdir -p "$SP"
while read -r name at url hash; do
	case $name in ''|'#'*) continue ;; esac
	file=${url##*/}
	fetch "$url" "$file" "${hash#--hash=sha256:}"
	case $file in
	*.whl) unzip -q -o "$WORK/downloads/$file" -d "$SP" </dev/null ;; # not the list this loop reads, if unzip asks
	*.tar.gz)
		rm -rf "$WORK/build/sdist" && mkdir -p "$WORK/build/sdist" "$SP/$name.dist-info"
		tar xzf "$WORK/downloads/$file" -C "$WORK/build/sdist" --strip-components=1
		for p in "$WORK/build/sdist"/lib/* "$WORK/build/sdist"/src/*; do
			if [ -f "$p/__init__.py" ]; then cp -R "$p" "$SP/"; fi
		done
		cp "$WORK/build/sdist"/LICENSE* "$SP/$name.dist-info/" ;;
	*) echo "unsupported archive $file" >&2; exit 1 ;;
	esac
done < "$HERE/requirements.txt"
# A wheel's scripts are command-line programs; nothing here runs them.
rm -rf "$SP"/*.data
# Pillow's and lxml's Python code, without lxml's Cython sources and tests. Each package's module that is built
# into python.wasm under a top-level name is a stub that puts the built-in module in its place.
cp -R "$PIL/PIL" "$SP/"
(cd "$LX/.." && tar cf - --exclude tests --exclude '*.c' --exclude '*.h' --exclude '*.pyx' --exclude '*.pxd' \
	--exclude '*.pxi' lxml) | (cd "$SP" && tar xf -)
# PIL.features reads the module's __file__, which a built-in module lacks.
printf 'import sys\nimport _imaging\n_imaging.__file__ = __file__\nsys.modules[__name__] = _imaging\n' > "$SP/PIL/_imaging.py"
printf 'import sys\nimport etree\nsys.modules[__name__] = etree\n' > "$SP/lxml/etree.py"
mkdir -p "$SP/pillow.dist-info" "$SP/lxml.dist-info"
cp "$WORK/src/pillow-13.0.0/LICENSE" "$SP/pillow.dist-info/"
cp "$WORK/src/lxml-6.1.3/LICENSE.txt" "$WORK/src/lxml-6.1.3/LICENSES.txt" "$SP/lxml.dist-info/"

# 7. Compile every module ahead of time. The sandbox mounts the library read-only and so never writes this cache
# itself; without it each import compiles its source again, which made `import openpyxl` take 3.7 s, not 0.7 s.
# unchecked-hash makes Python trust a compiled file whatever the source's time stamp, which unpacking changes.
"$HOSTPY" -m compileall -q --invalidation-mode unchecked-hash "$D/lib" >/dev/null
du -sk "$D/lib"
