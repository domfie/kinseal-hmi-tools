#!/bin/bash
set -euo pipefail
if [ "$(uname -s)" != Darwin ] || [ "$(uname -m)" != arm64 ]; then
  echo 'This build script targets macOS on Apple Silicon.' >&2
  exit 1
fi
uploader_dir="$(cd "$(dirname "$0")/.." && pwd)"
repo_dir="$(cd "$uploader_dir/.." && pwd)"
archive="$repo_dir/third_party/libusb-1.0.30.tar.bz2"
expected='fea36f34f9156400209595e300840767ab1a385ede1dc7ee893015aea9c6dbaf'
actual="$(shasum -a 256 "$archive" | cut -d ' ' -f 1)"
if [ "$actual" != "$expected" ]; then
  echo 'Unexpected libusb source archive hash.' >&2
  exit 1
fi
build_dir="$(mktemp -d "${TMPDIR:-/tmp}/kinseal-mac-build.XXXXXX")"
prefix="$build_dir/libusb-prefix"
out="${1:-$uploader_dir/dist/kinseal-uploader-macos-arm64-v0.2.7}"
mkdir -p "$prefix/lib" "$prefix/include/libusb-1.0" "$out/third_party"
tar -xjf "$archive" -C "$build_dir"
(
  cd "$build_dir/libusb-1.0.30"
  env MACOSX_DEPLOYMENT_TARGET=14.0 CC=clang ac_cv_func_pipe2=no ./configure     --prefix="$prefix" --disable-static --enable-shared     --disable-examples-build --disable-tests-build     CFLAGS='-O2 -mmacosx-version-min=14.0' LDFLAGS='-mmacosx-version-min=14.0'
  # Direct linking avoids older GNU libtool's handling of newer Darwin versions.
  clang -dynamiclib -O2 -std=gnu11 -fvisibility=hidden -mmacosx-version-min=14.0     -I. -Ilibusb -pthread libusb/core.c libusb/descriptor.c libusb/hotplug.c     libusb/io.c libusb/strerror.c libusb/sync.c libusb/os/events_posix.c     libusb/os/threads_posix.c libusb/os/darwin_usb.c     -framework IOKit -framework CoreFoundation -framework Security -lobjc     -Wl,-headerpad_max_install_names -Wl,-install_name,@rpath/libusb-1.0.0.dylib     -Wl,-compatibility_version,7.0.0 -Wl,-current_version,7.0.0     -o "$prefix/lib/libusb-1.0.0.dylib"
  cp libusb/libusb.h "$prefix/include/libusb-1.0/libusb.h"
  cp COPYING "$out/third_party/libusb-COPYING.txt"
)
ln -s libusb-1.0.0.dylib "$prefix/lib/libusb-1.0.dylib"
export MACOSX_DEPLOYMENT_TARGET=14.0
export CGO_ENABLED=1
export CGO_CFLAGS="-I$prefix/include -mmacosx-version-min=14.0"
export CGO_LDFLAGS="-L$prefix/lib -mmacosx-version-min=14.0"
(
  cd "$uploader_dir"
  env DYLD_LIBRARY_PATH="$prefix/lib" go test -race ./...
  go vet ./...
  GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o "$out/kinseal-uploader" .
)
cp "$prefix/lib/libusb-1.0.0.dylib" "$out/libusb-1.0.0.dylib"
cp "$archive" "$out/third_party/libusb-1.0.30.tar.bz2"
install_name_tool -change @rpath/libusb-1.0.0.dylib @executable_path/libusb-1.0.0.dylib "$out/kinseal-uploader"
codesign --force --sign - --timestamp=none "$out/libusb-1.0.0.dylib"
codesign --force --sign - --timestamp=none "$out/kinseal-uploader"
codesign --verify --strict "$out/kinseal-uploader"
"$out/kinseal-uploader" version
echo "Build prepared in: $out"
echo "Temporary build files remain in: $build_dir"
