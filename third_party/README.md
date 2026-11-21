# libusb 1.0.30

The native macOS uploader dynamically links libusb 1.0.30.
License: GNU LGPL 2.1 or later; see libusb-COPYING.txt.
The unmodified official source archive is included for rebuilding/replacing
the dependency, and the uploader source/build script is supplied.

Source: https://github.com/libusb/libusb/releases/tag/v1.0.30
Archive: https://github.com/libusb/libusb/releases/download/v1.0.30/libusb-1.0.30.tar.bz2
SHA256: fea36f34f9156400209595e300840767ab1a385ede1dc7ee893015aea9c6dbaf

The macOS release dylib was built with deployment target 14.0 and pipe2
detection disabled, since the build SDK exposes pipe2 only for newer macOS.
No libusb source changes were made. See uploader/scripts/build-macos.sh.
Keep the executable and dylib beside one another; the dylib can be replaced
with a compatible build. Windows packages do not depend on libusb.

## Other runtime notices

`Go-LICENSE.txt` preserves the Go Authors' BSD license for Go runtime and
standard-library components in the standalone executables.
`mingw-w64-COPYING.txt` preserves the applicable MinGW-w64 runtime notices
for the compiler-supplied Windows DLL runtime. These components are not
relicensed under the project's MIT license.
