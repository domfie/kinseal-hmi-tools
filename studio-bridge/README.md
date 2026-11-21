# KinSeal Studio 5.1 WinUSB Bridge

USB project downloads directly from KinSeal Studio 5.1 on Windows 11 ARM in
UTM on a Mac with Apple Silicon.

**Hardware-tested only with AMZ043W01 and the specific Studio 5.1 build
identified by this package.** The user confirmed that Studio downloads the
project and the display adopts the new version. Compatibility with other
models or Studio builds has not been confirmed.

This is an independent community project for interoperability with KinSeal
Studio. It is not published or endorsed by KinSeal. Studio itself and original
manufacturer binaries are not included in this release. Rights to third-party
software remain with their respective holders; any project license applies
only to the independently written components.

## What the replacement DLL does

The independently written `AMZUsb.dll` implements all seven original
`AmzUsb_*` exports with the same x86 cdecl interface. Studio continues to compile
the project, generate the KinSeal commands, show the download dialog and request
the display reboot.

USB transport now uses Windows WinUSB instead of `libusb0.dll` / `libusb0.sys`.
Each interrupt OUT write is limited to the endpoint's `wMaxPacketSize`, which is
**256 bytes** on the tested display. This matches the approach used by the
standalone uploader 0.2.5 confirmed working under UTM.

QEMU acknowledges interrupt OUT immediately, while usbredirhost rejects OUT
payloads larger than the endpoint's maximum packet size. Splitting writes
avoids that rejection. Sources: [QEMU](https://github.com/qemu/qemu/blob/master/hw/usb/redirect.c),
[usbredir](https://github.com/SPICEorg/usbredir/blob/master/usbredirhost/usbredirhost.c).
The exact component versions bundled with the tested UTM installation were
not recorded.

The DLL is **32-bit x86** because Studio is an x86 application, including when
running under Windows ARM emulation. It uses USB ID `7365:616C`, interface 0
and endpoints `0x01/0x81`. Other Studio backends are unchanged. It refuses to
connect if more than one matching display is present.

## WinUSB driver association

The [`driver`](driver/README.md) folder includes the **domfie 1.0.2.0 ARM64 INF**,
dated November 21, 2026, based on the working June package. It assigns Windows'
in-box Microsoft `winusb.sys` to `USB\VID_7365&PID_616C` and registers the device
interface GUID. The successful test system already had this association.

**The INF package is unsigned.** Windows may reject a new installation because
of the missing package signature. See the driver notes for details and the
Zadig alternative. Installing the driver association and replacing the Studio
DLL are separate operations.

## Update an existing Studio installation

1. Extract the release ZIP into a **separate folder**.
2. Close Studio. Assign the display to the **WinUSB** driver.
3. Right-click `patch-studio.bat` and select **Run as administrator**.
4. Start Studio normally and use its USB project download.

Default installation directory:

```text
C:\Program Files (x86)\KinSealStudio5.1
```

The batch file uses Windows' built-in PowerShell. It checks fixed SHA-256
hashes of Studio, the original DLL and the replacement DLL, backs up the
original as `AMZUsb.vendor.dll`, then replaces `AMZUsb.dll`. An existing valid
backup is preserved. Unknown files cause an abort. Running the installer again
with the bridge already installed is supported. The Studio EXE and USB driver
are not modified by this batch file.

For a different installation directory, use an elevated Command Prompt:

```bat
patch-studio.bat "D:\Programs\KinSealStudio5.1"
```

To restore the original DLL, close Studio and run as administrator:

```bat
patch-studio.bat /restore
```

Or specify a different directory:

```bat
patch-studio.bat /restore "D:\Programs\KinSealStudio5.1"
```

The restored manufacturer DLL requires the original libusb0 driver association.

## Compatibility and diagnostics

Automatic installation is restricted to the exact Studio 5.1 build analyzed
for this project. Its known hashes are in the batch file and `BUILD-INFO.json`.
A different Studio build requires an ABI compatibility check before updating
the hash pins. The installation path alone does not establish compatibility.

The integrated Studio project download is user-confirmed only for **AMZ043W01**
under Windows ARM / UTM. The batch installer was statically checked but has not
yet been executed on a Windows installation.

Studio controls the download scope. Use the same project-download options as
in the successful reference test. Firmware and font updates have not been
verified with the bridge.

Diagnostic log:

```text
%TEMP%\kinseal-studio-winusb.log
```

It records call lengths, return values and Windows error codes, not project
payloads. The DLL does not require Go, libusb0 or additional bundled runtime DLLs.

Alternatively, `Install-StudioBridge.ps1 -StudioPath 'C:\Path\KinSealStudio5.1'`
creates a separate Studio copy under `%LOCALAPPDATA%\KinSealStudio5.1-WinUSB`.
That script and `Restore-StudioBridge.ps1` support the separate-copy workflow;
use the batch file above to update an existing installation in place.

## Adapting the bridge to another display

The bridge implements USB transport. Studio still generates the model-specific
commands and project data. Another model using the same `AMZUsb.dll` ABI and USB
layout may be compatible, but this must be verified on that model. Renaming the
DLL or device alone does not adapt an incompatible interface.

### 1. Capture a working reference and USB descriptors

Record a successful original Studio download, for example with Wireshark and
USBPcap on a working Windows PC. Determine the hardware ID, interface, alternate
setting, endpoint addresses, transfer types and maximum packet sizes. Hardware
IDs appear in Device Manager under **Details → Hardware IDs**. USB descriptors
in the capture provide the endpoint information.

| Property | Tested AMZ043W01 | Source location |
|---|---|---|
| Vendor/product ID | `7365:616C` | `bridge.c`: `connect_locked()` |
| Interface / alternate index | `0 / 0` | `bridge.c`: `open_path()` |
| OUT endpoint | `0x01` | `open_path()`, `set_policy()` calls, `write_one()` |
| IN endpoint | `0x81` | `open_path()`, `set_policy()` calls, `AmzUsb_readRaw()` |
| Endpoint type | Interrupt in both directions | Validation in `open_path()` |
| Maximum OUT packet size | `256 bytes` | Read from `MaximumPacketSize` |
| Device interface GUID | `{F5970E6C-EBEE-424F-9249-8FECBAAB028F}` | `driver/domfie_HMI_WinUSB_ARM64.inf` |

The bridge identifies the device using its hardware ID and registered interface
paths. It does not search by the friendly name shown in Device Manager.

### 2. Verify the DLL and ABI expected by Studio

Check that Studio actually loads `AMZUsb.dll` for the new model. Some models
use another backend, such as `NewUsb.dll`. Export names, parameters, x86 calling
convention and return values must match the backend expected by Studio.

`AmzUsb_readRaw()` returns the byte count. `AmzUsb_writeRaw()` returns `1` after
a complete write, `0` on a transfer failure and `-1` when disconnected. A
replacement must preserve those conventions. Another backend requires its own
ABI inspection and, where necessary, implementation.

### 3. Update device discovery and endpoint handling

Change the hardware-ID prefix in `connect_locked()` in `bridge.c`. Update the
compared prefix length as well. Composite devices with an additional interface
component in their instance ID require corresponding discovery changes.
Update endpoint addresses at **every** location listed in the table so that
validation, policies and transfers all use the same pipes.

The current implementation queries alternate index 0 and requires interface
number 0. Another interface or alternate setting requires suitable selection
and, where needed, activation code. The released DLL does not implement those
alternatives. Validation accepts a matching bulk or interrupt endpoint pair;
isochronous transfers or other layouts need a different implementation.

Do not hard-code `256` for a different interrupt device. `open_path()` reads
its maximum OUT packet size from the descriptor. `packet_write.h` limits writes
to that value, sending the final shorter block without padding. Bulk splitting
is left to WinUSB in this implementation.

### 4. Update the INF and installation scripts

Change the hardware ID and, if appropriate, display names in
`driver/domfie_HMI_WinUSB_ARM64.inf`. The included INF contains an ARM64 model
section only. A new device association may use its own interface GUID. The
bridge reads registered GUIDs from the device registry; its hard-coded GUIDs
are fallbacks. The registered GUID must belong to the selected device instance.

After editing the INF, update its pinned SHA-256 in `driver/Install-WinUSB.ps1`.
That script's `USB\VID_7365&PID_616C\*` filter must match the new hardware too.
Update the INF version/date and build metadata. The included INF is unsigned;
changing names or IDs does not add a package signature.

### 5. Build the DLL and update package hashes

Build as described below. Keep the **x86** target for the analyzed 32-bit Studio,
including on Windows ARM. Update hashes in `patch-studio.bat`, `BUILD-INFO.json`
and any installation/restoration metadata used. Change the Studio hash pins
only after verifying compatibility with that exact build; do not simply remove
the checks.

To calculate a hash on Windows:

```powershell
Get-FileHash .\AMZUsb.dll -Algorithm SHA256
```

### 6. Verify the new model on hardware

Use a separate Studio copy and a known project first. Change a clearly visible
project marker, download it and check the physical display after reboot.
A download dialog, completed writes or a positive status response alone do not
prove that the project was adopted. Check the required pages, touch controls
and communication functions. Record the exact model, Studio build, Windows/VM
version and result. The packet-core tests do not replace a hardware test.

## Build and tests

With Zig 0.17.1 as the C cross-compiler:

```sh
zig cc -target x86-windows-gnu -shared -O2 -Wall -Wextra -Werror \
  -o AMZUsb.dll bridge.c -lsetupapi -lwinusb -ladvapi32 -lole32 -luser32
```

Native packet-core tests, for example with Clang on macOS:

```sh
clang -std=c11 -Wall -Wextra -Werror -fsanitize=address,undefined \
  packet_write_test.c -o packet_write_test
./packet_write_test
```

Tests cover byte order, packet sizes, empty input and stopping without retries
on partial or failed writes. Export names, x86 PE format and SetupAPI structure
sizes were also checked when building the DLL.

## Distribution

The integration package contains the independently written DLL, C sources,
tests, installation scripts, domfie INF and build metadata. Manufacturer EXEs,
original manufacturer DLLs, Studio resources and user projects are not included.

## License

The independently written components are licensed under the MIT License in
`LICENSE`. This does not relicense KinSeal Studio or other third-party software.
Third-party components retain their separate licenses and notices.
