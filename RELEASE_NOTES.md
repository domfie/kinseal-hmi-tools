# KinSeal tools for Windows 11 ARM / UTM and Mac Apple Silicon

Hardware-tested model: **AMZ043W01 only**.

- Studio bridge **0.1.0**: normal Studio USB Download button with WinUSB;
  user-confirmed working on Windows ARM in UTM.
- Windows x86 uploader **0.2.5**: unchanged tested binary, also working under
  Windows ARM x86 emulation. WinUSB required.
- macOS Apple Silicon uploader **0.2.7**: new native libusb backend, packet-sized
  interrupt writes and optional kernel-driver detach. **Native display upload
  confirmed by the user with sudo and --detach-kernel-driver.** Build target macOS 14+;
  dependency included, no Homebrew installation needed for the release ZIP.
- domfie ARM64 WinUSB INF **1.0.2.0**: device name `Smart Control Device`.
  The INF is unsigned; see driver installation notes.

The Studio bridge and standalone uploader split interrupt OUT at the actual
endpoint maximum (256 bytes on the tested display), avoiding oversized writes
through UTM/QEMU usbredir. DLL installers include backup/restore and hash checks.

The upload replaces the project. Confirm the new version directly on the
display after reboot. Other models, Studio builds and firmware/font updates
have not been verified. Mac binaries are ad-hoc signed, not notarized.

## macOS 0.2.7 configuration fix

Handles a missing active configuration descriptor by reading the advertised
configuration. Probe/upload select and confirm it only when it is unique,
has the expected USB layout and the device is currently unconfigured.
The read-only usb-info command can report the advertised descriptor without
configuration changes. Regression tests cover the reported NOT_FOUND failure,
read-only diagnostics and rejection of ambiguous/failed configuration changes.
The subsequent privileged native macOS upload was confirmed successful by the
user on AMZ043W01.

## Documentation and diagnostics update

The current macOS uploader requires sudo and explicit kernel-driver detach on
the tested setup. Direct HID access without sudo was investigated, but no
model reply was received through native IOKit or HIDAPI. This is not a working
rootless alternative. Diagnostic sources/results are included in the repository.
The tested executables, Studio bridge DLL and driver remain unchanged.
