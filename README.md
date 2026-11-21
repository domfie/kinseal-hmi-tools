# KinSeal HMI Tools: Windows 11 ARM, UTM and Mac Apple Silicon

WinUSB bridge for **KinSeal Studio 5.1 on Windows 11 ARM in UTM on a Mac**,
plus standalone project uploaders for Windows x86 and native Apple Silicon macOS.

**Only AMZ043W01 has been hardware-tested.** Other display models and other
Studio builds require validation. Native macOS uploads are now user-confirmed
with `sudo` and `--detach-kernel-driver`.

| Component | Platform | Status |
|---|---|---|
| Studio WinUSB bridge 0.1.0 | 32-bit Studio on Windows 11 ARM / UTM | Display update confirmed by user |
| Standalone uploader 0.2.5 x86 | Windows PC; also Windows ARM via x86 emulation | Display update confirmed, including UTM |
| Standalone uploader 0.2.7 macOS arm64 | Mac with Apple Silicon, build target macOS 14+ | Display update confirmed with sudo and kernel-driver detach |
| domfie WinUSB INF 1.0.2.0 | Windows ARM64 | Updated package metadata; unsigned INF |

## Independent project and third-party rights

This is an independent community project for interoperability with KinSeal
Studio. It is not published or endorsed by KinSeal. KinSeal Studio and original
manufacturer binaries are not included. Rights to KinSeal software and other
third-party components remain with their respective holders. Any project
license applies only to the independently written components and does not
relicense KinSeal Studio or libusb.

## Downloads

Get the appropriate ZIP from this repository's **Releases**:

- `kinseal-studio-winusb-github.zip`: Studio replacement DLL, patch/restore batch,
  project source code and the domfie ARM64 WinUSB INF.
- `kinseal-uploader-windows-x86-v0.2.5.zip`: standalone Windows uploader. This is
  the unchanged tested x86 binary; it also runs under Windows ARM emulation.
- `kinseal-uploader-macos-arm64-v0.2.7.zip`: native Mac uploader with its libusb
  dependency, source and third-party license/source archive included.

These packages contain our tools. Studio itself, manufacturer firmware and
user projects are not bundled. A fresh Studio-compiled `load.kns` is required.

## Use the normal Studio Download button

See [Studio bridge instructions](studio-bridge/README.md).
Keep WinUSB assigned to `Smart Control Device` (`7365:616C`), close Studio and
run `studio-bridge/patch-studio.bat` as administrator. The default target is:

```text
C:\Program Files (x86)\KinSealStudio5.1
```

The original DLL is backed up; `/restore` restores it. The patcher checks the
specific known Studio/DLL hashes. The underlying Windows `winusb.sys` remains
Microsoft's system driver. The optional domfie INF is unsigned; see the
[driver notes](studio-bridge/driver/README.md) and the Zadig alternative.

## Standalone Windows uploader

Extract the Windows ZIP. With WinUSB assigned and Studio closed:

```powershell
.\kinseal-uploader.exe list
.\kinseal-uploader.exe inspect --file .\load.kns
.\kinseal-uploader.exe probe --out .\display-windows.json
.\kinseal-uploader.exe upload --file .\load.kns --probe .\display-windows.json --model AMZ043W01 --experimental --log .\upload-windows.jsonl
```

The upload replaces the display project and requests a reboot. Confirm the
new UI on the physical display; API byte counts alone are insufficient.

## Native Mac / Apple Silicon uploader

See [native macOS instructions](MACOS.md). Disconnect the display from UTM so
macOS has exclusive access. Keep the executable and supplied dylib together.

```sh
./kinseal-uploader list
./kinseal-uploader inspect --file ./load.kns
sudo ./kinseal-uploader probe --detach-kernel-driver --out ./display-mac-root.json
sudo ./kinseal-uploader upload --detach-kernel-driver --file ./load.kns --probe ./display-mac-root.json --model AMZ043W01 --experimental --log ./upload-mac.jsonl
```

**The current Mac uploader requires `sudo` for `probe` and `upload` on the
hardware-tested setup**, together with `--detach-kernel-driver`. macOS owns the
HID interface, so libusb needs elevated privileges to take temporary ownership.
`list` and `inspect` do not require sudo. The executable and library are ad-hoc
signed, not Developer ID signed or notarized.

Direct HID access without sudo was investigated on AMZ043W01. Opening the
interface and submitting the model query succeeded, but neither native IOKit
nor HIDAPI received a reply, including shared/exclusive opens and 32-/256-byte
outputs. A rootless uploader is therefore **not available in this release**.
See the [HID investigation](uploader/analysis/hid-probe/README.md) for the results.
The Studio bridge continues to run inside Windows.

## Why 256-byte writes matter in UTM

The tested display exposes interrupt endpoints with a 256-byte maximum packet
size. QEMU usbredir acknowledges interrupt OUT immediately, while usbredirhost
rejects OUT payloads larger than the endpoint maximum. Large writes can therefore
appear successful in the guest without reaching the display. These tools split
interrupt writes at the descriptor's maximum packet size. The user confirmed
that this resolved the Windows ARM / UTM upload failure.

Sources: [QEMU redirect.c](https://github.com/qemu/qemu/blob/master/hw/usb/redirect.c),
[usbredirhost.c](https://github.com/SPICEorg/usbredir/blob/master/usbredirhost/usbredirhost.c).
The exact component versions installed with the tested UTM were not recorded.

## Source and adaptation

- [`studio-bridge`](studio-bridge/README.md): C replacement DLL, installer and
  detailed adaptation guide for another display model.
- [`uploader`](uploader/README.md): Go CLI and Windows/macOS USB backends.
- [`MACOS.md`](MACOS.md): native Mac usage and limitations.
- [`PUBLISHING.md`](PUBLISHING.md): GitHub setup, description, topics and releases.

The independently written components are licensed under [MIT](LICENSE).
See [LICENSING.md](LICENSING.md) for the scope and third-party notices.
The bundled libusb dependency retains its LGPL license; see
[`third_party`](third_party/README.md).
