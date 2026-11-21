# KinSeal project uploader

A Go command-line uploader for compiled KinSeal Studio projects, with direct
WinUSB access on Windows and a libusb backend on macOS.

**Hardware-tested only with AMZ043W01.** The Windows x86 uploader 0.2.5 was
confirmed working on a Windows PC and under Windows ARM emulation in UTM on
an Apple Silicon Mac. Version 0.2.6 introduced native macOS support; version 0.2.7 handles missing
active configuration descriptors. A native macOS upload is now user-confirmed
with sudo and --detach-kernel-driver on AMZ043W01.

This is an independent interoperability project, not a KinSeal product or an
endorsed Studio update. Studio itself, original manufacturer DLLs, firmware
and user projects are not distributed with this tool. Any project license
applies only to the independently written components; third-party rights and
licenses remain separate.

## Supported input and scope

Use a fresh Studio-compiled `load.kns`, or a ZIP containing one unambiguous
compiled `load.kns`. File signatures determine the format, not the extension.
Raw project source (`.sm`) and arbitrary firmware binaries are not accepted.

A wrapper ZIP may contain both `load.kns` and individual compiled files. These
must match; mixed export versions are rejected before USB access. Recompile
and use the fresh `load.kns` directly if you change the project.

The uploader sends project data such as pages, graphics, macros and recipes.
Manufacturer firmware and font updates are excluded. The analyzed AMZ profile
currently supports one COM1 protocol driver and rejects unsupported archive
entries/layouts. ControlPara data larger than the validated 256 KiB path is
not enabled. Project preparation and inspection do not access USB.

## Windows

Assign the device to **WinUSB** and close Studio. The uploader does not install
or change drivers. The original libusb0 device association is not supported
by this backend. For the tested display, the USB ID is `7365:616C`.

```powershell
.\kinseal-uploader.exe version
.\kinseal-uploader.exe list
.\kinseal-uploader.exe inspect --file .\load.kns
.\kinseal-uploader.exe probe --out .\display.json
.\kinseal-uploader.exe upload --file .\load.kns --probe .\display.json --model AMZ043W01 --experimental --log .\upload.jsonl
```

The release package contains the unchanged, tested Windows x86 binary 0.2.5.
The shared source is 0.2.7, which includes the Darwin backend. Windows builds do
not depend on libusb or CGo. The Studio integration is a separate replacement
DLL described in the repository's `studio-bridge` directory.

## Native macOS / Apple Silicon

The release ZIP includes libusb 1.0.30. Keep the executable and dylib in the
same directory. Build target: macOS 14+. Program startup, inspection, enumeration
and host tests passed. A native project upload is user-confirmed with sudo
and kernel-driver detach; older macOS versions still need verification.

Disconnect USB forwarding to UTM and close other applications using the display:

```sh
./kinseal-uploader list
./kinseal-uploader inspect --file ./load.kns
./kinseal-uploader probe --out ./display-mac.json
./kinseal-uploader upload --file ./load.kns --probe ./display-mac.json --model AMZ043W01 --experimental --log ./upload-mac.jsonl
```

If a macOS HID driver owns the interface, libusb may need root privileges and
explicit permission to detach it temporarily:

```sh
sudo ./kinseal-uploader probe --detach-kernel-driver --out ./display-mac-root.json
sudo ./kinseal-uploader upload --detach-kernel-driver --file ./load.kns --probe ./display-mac-root.json --model AMZ043W01 --experimental --log ./upload-mac-root.jsonl
```

The option can trigger USB re-enumeration; libusb attempts to reattach the
driver on close. No detachment is requested by default. The released binaries
are ad-hoc signed, not Developer ID signed or notarized.

## macOS configuration selection (0.2.7)

An unconfigured device may return `LIBUSB_ERROR_NOT_FOUND` when the backend
asks for its active configuration descriptor. Probe/upload now read the
advertised descriptor, validate the expected interface and endpoints and
select its bConfigurationValue only if exactly one configuration is available
and the device's current configuration value is zero. The active descriptor
is then read again before claiming the interface. Existing active
configurations are preserved. Read-only usb-info never selects a configuration.
The configuration path is regression-tested. A direct hardware test passed the
original descriptor failure and reached interface claiming, where macOS returned
LIBUSB_ERROR_ACCESS. The subsequent privileged probe/upload with explicit
kernel-driver detach was confirmed successful by the user.

## Diagnostics and target checks

- `list`: enumerate candidate devices. `--all` includes other USB IDs.
- `inspect --file ...`: validate the package and print the transfer plan offline.
- `usb-info --out usb-info.json`: read USB metadata without KinSeal commands.
  Windows includes raw configuration descriptors; macOS reports parsed
  interfaces. If the active descriptor is missing, it can show the sole
  advertised configuration, labeled `advertised-only`, without configuring it.
- `probe --out display.json`: query model/parameters without starting a project
  download. Output must be a new filename.
- `upload`: re-identify the selected device, verify it against the probe report,
  transfer project segments, request commit and send reset.

Use `--path` from `list` when several devices match. `--vid` and `--pid` allow
explicit IDs, but do not establish model compatibility. `--guid` applies only
to a registered Windows interface GUID. A probe report binds the upload to
the selected device instance/path; create it on the target operating system.

`AMZ043W01` requires model code `60 / 0x3C` and the explicit `--experimental`
flag. That flag does not bypass a mismatched known model code or unsupported
archive layout. The model name must match the compiled project's model.

The upload replaces the project and requests a reboot. It does not back up
the existing project. The result reports command acknowledgments and reset
submission, with `screen_verified: false`. Confirm the changed UI on the
physical display: API byte counts and positive replies alone do not prove
that the project was installed. Partial or failed transfers stop without
blind retries.

## Why packet-sized interrupt writes are used

The tested device exposes interrupt endpoints `0x01/0x81`, maximum packet size
256 bytes. Earlier builds sent large payloads in single API calls, which
appeared successful in UTM but did not update the display. Version 0.2.5
limits each interrupt OUT call to the descriptor's maximum packet size. The
user confirmed this fixed the UTM / Windows ARM upload. Bulk handling remains
unchanged.

QEMU usbredir acknowledges interrupt OUT immediately; usbredirhost rejects
OUT payloads larger than the endpoint maximum. See
[QEMU](https://github.com/qemu/qemu/blob/master/hw/usb/redirect.c) and
[usbredir](https://github.com/SPICEorg/usbredir/blob/master/usbredirhost/usbredirhost.c).
The exact bundled UTM component versions were not recorded. Investigation
results and capture limitations are documented in `analysis/`.

## Build and tests

Build a Windows x86 executable from the shared source:

```sh
GOOS=windows GOARCH=386 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o kinseal-uploader.exe .
```

For the portable native Mac build, use the prepared repository with the
included libusb source archive:

```sh
./scripts/build-macos.sh
```

It builds libusb for macOS 14+, runs the host tests, builds the arm64 executable,
sets a relative library path and applies ad-hoc signatures. It requires Go and
Apple's C command-line tools. The executable and dylib are placed in `dist/`.

For a normal local development build, the Darwin backend expects libusb headers
and libraries under `/opt/homebrew`, or suitable `CGO_CFLAGS` / `CGO_LDFLAGS`.
The portable build script supplies its own library paths instead.

```sh
go test -race ./...
go vet ./...
```

The optional local export test uses `KINSEAL_TEST_EXPORT` to read a supplied
reference export. That proprietary export is not included. Tests cover archive
validation, command framing, packet-sized delivery and failure boundaries.
Mocks and host tests do not replace a successful hardware upload.

## License

The independently written components are licensed under the MIT License in
`LICENSE`. This does not relicense KinSeal Studio or other third-party software.
Third-party components retain their separate licenses and notices.

## Prepared publication package and local archive

The current GitHub upload folder is `dist/kinseal-tools-github/repository`.
Attach only the ZIPs and SHA256SUMS from its adjacent `release-assets` folder
to a GitHub Release. The complete handoff is `dist/kinseal-tools-github.zip`.
Older builds, extracted test runs and local project/log files are kept under
`dist/archive`; this private archive is not part of the publication package.

**The current Mac uploader requires sudo for probe/upload on the tested
AMZ043W01 setup**, with `--detach-kernel-driver`. Direct HID access without
sudo was tested but did not return a model reply; see
[the investigation](analysis/hid-probe/README.md). No working rootless transport
is included in v0.2.7.
