# domfie WinUSB device association

`domfie_HMI_WinUSB_ARM64.inf` is based on the working June 2026 WinUSB INF.
Its provider and manufacturer are `domfie`; its device name is
`Smart Control Device`.

- Provider and manufacturer: `domfie`
- Driver date: November 21, 2026
- Package version: `1.0.2.0`
- Hardware ID: `USB\VID_7365&PID_616C`
- Interface GUID: `{F5970E6C-EBEE-424F-9249-8FECBAAB028F}`
- Target: Windows ARM64

The working WinUSB association has been confirmed only for **AMZ043W01** under
Windows ARM / UTM. Other models have not been tested. See
[Adapting the bridge to another display](../README.md#adapting-the-bridge-to-another-display).

The USB association and GUID are unchanged from the June package. The updated
metadata and renamed INF have not yet been reinstalled under Windows.

The package assigns Microsoft's in-box `winusb.sys` to the device. It contains
no custom kernel driver and does not redistribute Microsoft's SYS file. The
Windows driver-file version is independent of the INF package version.

## Existing working installations

If the display already uses WinUSB and Studio downloads work, no driver change
is needed for functionality. The old provider name will remain visible.
`install-winusb.bat` detects this situation and preserves the association.

## Update the displayed provider

Run as administrator:

```bat
install-winusb.bat /update
```

This bypasses the check that normally skips an existing WinUSB association.
Windows can still reject the unsigned package; the script reports that outcome.

## New installations

**The INF is unsigned and has no signed catalog.** The Microsoft SYS file has
its own separate signature. Windows may reject the INF package on another PC
because the package is unsigned. This is not a generally installable,
Microsoft-signed driver distribution.

[Zadig](https://zadig.akeo.ie/) is an alternative for assigning WinUSB: close
Studio, select `Smart Control Device` with USB ID `7365:616C`, and install
**WinUSB**. The bridge can discover another GUID registered by Zadig through
the device registry. Zadig can also provide a WinUSB association on x86/x64;
the included INF itself has an ARM64 model section only.

To attempt installation of the included domfie INF, run `install-winusb.bat`
as administrator. It invokes `pnputil /add-driver ... /install`, checks the
exit code and then checks the device service. It does not change signature or
security settings and does not restart Windows automatically. The script has
been statically checked but has not yet been executed under Windows.

PnPUtil does not force an association if another driver has a higher rank.
See [Microsoft: PnPUtil](https://learn.microsoft.com/en-us/windows-hardware/drivers/devtest/pnputil-command-syntax)
and [WinUSB installation](https://learn.microsoft.com/en-us/windows-hardware/drivers/usbcon/winusb-installation).

The driver association and Studio DLL replacement are separate steps: assign
WinUSB first, then run `patch-studio.bat` in the parent directory.

## License

The independently written components are licensed under the MIT License in
`LICENSE`. This does not relicense KinSeal Studio or other third-party software.
Third-party components retain their separate licenses and notices.
