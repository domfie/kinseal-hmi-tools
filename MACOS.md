# Native uploader for Mac with Apple Silicon

Version 0.2.7 is a native macOS arm64 uploader. **A complete native project
upload to AMZ043W01 has now been confirmed by the user**, using `sudo` and
`--detach-kernel-driver`. All 633178 project payload bytes were reported sent,
the commit was acknowledged and reset was submitted; the user reported that
the upload worked. AMZ043W01 remains the only hardware-tested model.

Build target: macOS 14 or newer on Apple Silicon. It has not been run on every
supported macOS version. The ZIP includes libusb 1.0.30; Homebrew is not required
to run the release. Keep `kinseal-uploader` and `libusb-1.0.0.dylib` together.

## Usage

Make the display available to macOS: disconnect its USB forwarding to UTM and
close other applications using it. The Studio replacement DLL runs inside
Windows; this package is a separate native command-line uploader. You still
need a fresh `load.kns` compiled by Studio.

Open Terminal in the extracted directory:

```sh
chmod +x ./kinseal-uploader
./kinseal-uploader version
./kinseal-uploader list
./kinseal-uploader inspect --file ./load.kns
sudo ./kinseal-uploader probe --detach-kernel-driver --out ./display-mac-root.json
sudo ./kinseal-uploader upload --detach-kernel-driver --file ./load.kns --probe ./display-mac-root.json --model AMZ043W01 --experimental --log ./upload-mac.jsonl
```

`probe` creates a new file; use a new filename when repeating it. Windows probe
reports cannot be reused on macOS. `list` and `usb-info` read device metadata.
On macOS, `usb-info` reports parsed interfaces, not raw configuration bytes.
If no active descriptor is available, it can show the sole advertised
configuration with `configuration_source: advertised-only`, without opening
or reconfiguring the device. `upload` replaces the project and requests a reboot.
Verify the changed project on the physical display afterward.

## USB configuration initialization

Version 0.2.7 fixes the startup failure
`libusb_get_active_config_descriptor: LIBUSB_ERROR_NOT_FOUND (-5)` observed
when macOS/libusb reports no active configuration. Before `probe` or `upload`,
the backend may select the sole advertised configuration if it contains the
expected interface and endpoints and the current configuration value is zero.
The value comes from the descriptor, rather than being hard-coded to 1.
The backend then reads the active descriptor again before claiming the
interface or sending any KinSeal command. It does not change an already active
configuration or select between multiple configurations. `usb-info` remains
read-only. The initial non-root test then encountered `LIBUSB_ERROR_ACCESS`
at interface claiming. The subsequent privileged test, with explicit kernel-
driver detach, completed the upload successfully according to the user.

## Current sudo requirement and HID investigation

**The current Mac uploader requires `sudo` for `probe` and `upload` on the
tested AMZ043W01 setup**, with `--detach-kernel-driver`. `list` and `inspect`
can run as an ordinary user. No rootless replacement is included in v0.2.7.

A direct HID transport was tested as a normal user with native IOKit and
HIDAPI 0.15.0. Opening the device and writing the model query succeeded, but
no model reply arrived with either 32-byte or padded 256-byte outputs, using
shared or exclusive access. No project download or reset was attempted in
these tests. The cause remains unresolved; write success alone does not
establish a usable transport. The working libusb backend is unchanged.

In the source repository, see `uploader/analysis/hid-probe/README.md` for
reproduction instructions and observations.

## When macOS owns the USB interface

The tested Mac setup required the privileged commands above. If your interface
is available without a kernel driver, `sudo` and the detach option may not be
needed. The `--experimental` upload flag is still required by the CLI.

For `LIBUSB_ERROR_BUSY`, first check whether UTM or another application still
owns the device. If a macOS HID driver is attached, libusb may need root
privileges to take temporary ownership. Enable this explicitly:

```sh
sudo ./kinseal-uploader probe --detach-kernel-driver --out ./display-mac-root.json
sudo ./kinseal-uploader upload --detach-kernel-driver --file ./load.kns --probe ./display-mac-root.json --model AMZ043W01 --experimental --log ./upload-mac-root.jsonl
```

This option allows libusb to detach the kernel driver temporarily and reattach
it on close. This can trigger USB re-enumeration. Without the option, the
uploader does not request detachment. The package does not disable system-wide
security settings.

## Signing and dependency

The executable and supplied library are ad-hoc signed. They are not signed
with an Apple Developer ID and are not notarized. macOS may require manual
approval for downloaded files.

libusb 1.0.30 is dynamically linked and licensed under LGPL 2.1 or later. Its
unchanged license and official source archive are included. The runtime library
path is relative to the executable, so the local Homebrew installation and
its paths are not required.
