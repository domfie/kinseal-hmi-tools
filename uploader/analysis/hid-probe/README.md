# Native macOS HID investigation

Tested on the connected AMZ043W01, Apple Silicon, macOS 27.2.1 (26B131).
All hardware tests ran as UID/EUID 501, without sudo. The execution sandbox
hid USB devices, so the tests ran outside that sandbox, still as the ordinary
user. No project download, start, commit, reset or kernel-driver detach command
was sent. The working v0.2.7 libusb uploader is unchanged.

## Result

**Opening HID without root works; a working KinSeal request/reply exchange
through HID has not been established.** This is not proof that HID can never
work, but it is insufficient to replace the current transport.

- USB VID/PID: `7365:616c`, interface 0, vendor HID usage page `0xff06`, usage 1.
- The HID descriptor advertises 256-byte input and output reports, without
  report IDs, and an 8-byte feature report.
- Native `IOHIDDeviceOpen` succeeded. The input callback received no reports
  following the model query, for either a 32-byte output or a zero-padded
  256-byte output. Both `IOHIDDeviceSetReport` calls returned success.
- Independently, official HIDAPI 0.15.0 produced the same result with both
  shared and exclusive HID opens. `hid_write` succeeded for both lengths;
  `hid_read_timeout` returned 0 after 3000 ms each time. HIDAPI's leading
  zero report-ID placeholder is stripped before the native call; it is not
  an additional byte in the KinSeal protocol payload.
- When the device initially had configuration zero, selecting its sole
  advertised configuration (1) without root allowed HID enumeration. That
  alone did not make the model query work.

Only `KinSealStudioKiss`, command `0x31`, option `0x02` was sent. A successful
model reply would contain 24 bytes starting with model code `0x3c`.
The raw HID report descriptor was:

```
0606ff0901a101150026ff007508960001090181029600010901910295080901b102c0
```

The cause remains unresolved. The difference between advertised 256-byte HID
reports and the protocol's 24-byte model reply / 1-byte acknowledgements is a
possible compatibility issue, not a demonstrated explanation. No USB capture
of these HID requests was obtained. Do not infer device-side receipt from a
successful SetReport/write return value.

Apple's older open-source USB HID driver attempts interrupt OUT for output
reports before falling back to control SET_REPORT. Therefore it would be
incorrect to claim, without evidence on this system, that macOS HID always
uses the wrong endpoint. Its behavior on this macOS release has not been
established from that older source.

## Reproduce the independent test

The prototype is our MIT code. The third-party implementation is downloaded
and compiled separately; no HIDAPI source is vendored here.

Official source archive:
https://github.com/libusb/hidapi/archive/refs/tags/hidapi-0.15.0.tar.gz

SHA256: `5d84dec684c27b97b921d2f3b73218cb773cf4ea915caee317ac8fc73cef8136`

With the archive extracted into `/private/tmp`:

```sh
clang -std=c11 -Wall -Wextra -Werror -mmacosx-version-min=14.0 \
  -I/private/tmp/hidapi-hidapi-0.15.0/hidapi \
  -I/private/tmp/hidapi-hidapi-0.15.0/mac \
  hidapi_probe.c /private/tmp/hidapi-hidapi-0.15.0/mac/hid.c \
  -framework IOKit -framework CoreFoundation \
  -o /private/tmp/kinseal-hidapi-probe
/private/tmp/kinseal-hidapi-probe
/private/tmp/kinseal-hidapi-probe --exclusive
```

The display must be available to macOS and have an active configuration.
`hid_probe.c` is the separate native IOKit/libusb diagnostic; its optional
`--configure` selects the advertised configuration only when current value
is zero. It also refuses root execution.

## References

- HIDAPI macOS implementation:
  https://github.com/libusb/hidapi/blob/hidapi-0.15.0/mac/hid.c
- Apple IOHIDDeviceOpen:
  https://developer.apple.com/documentation/iokit/1588670-iohiddeviceopen
- Older Apple USB HID driver (not evidence of the current binary's behavior):
  https://github.com/opensource-apple/IOUSBFamily/blob/master/IOUSBHIDDriver/Classes/IOUSBHIDDriver.cpp

Raw observations are saved alongside this file in `*-result.txt`. These are
diagnostics, not a new uploader release or a successful rootless upload test.
