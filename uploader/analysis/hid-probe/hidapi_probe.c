// SPDX-License-Identifier: MIT
// Model query only: no download/start/commit/reset, no kernel-driver detach.
#include <hidapi.h>
#include <hidapi_darwin.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

int main(int argc, char **argv) {
    int exclusive = argc == 2 && strcmp(argv[1], "--exclusive") == 0;
    setvbuf(stdout, NULL, _IOLBF, 0);
    printf("uid=%u euid=%u hidapi=%s\n", getuid(), geteuid(), hid_version_str());
    if (geteuid() == 0) return 2;
    if (hid_init() != 0) return 2;
    hid_darwin_set_open_exclusive(exclusive);
    printf("exclusive=%d\n", exclusive);
    struct hid_device_info *devices = hid_enumerate(0x7365, 0x616c);
    unsigned count = 0;
    for (struct hid_device_info *d = devices; d; d = d->next) {
        printf("device path=%s usage_page=0x%04x usage=%u interface=%d\n", d->path, d->usage_page, d->usage, d->interface_number);
        count++;
    }
    if (count != 1) { printf("expected one device, found %u\n", count); hid_free_enumeration(devices); hid_exit(); return 1; }
    hid_device *device = hid_open_path(devices->path);
    hid_free_enumeration(devices);
    if (!device) { printf("open failed: %ls\n", hid_error(NULL)); hid_exit(); return 1; }
    int success = 0;
    const size_t sizes[] = {32, 256};
    for (unsigned n = 0; n < 2 && !success; n++) {
        unsigned char output[257] = {0}; // Byte 0 is HIDAPI's report-ID placeholder.
        memcpy(output + 1, "KinSealStudioKiss", 17);
        output[18] = 0x31;
        output[19] = 2;
        int written = hid_write(device, output, sizes[n] + 1);
        printf("model_query payload=%zu hid_write=%d error=%ls\n", sizes[n], written, hid_error(device));
        if (written < 0) continue;
        unsigned char input[4096];
        int length = hid_read_timeout(device, input, sizeof(input), 3000);
        printf("hid_read_timeout=%d", length);
        if (length > 0) {
            printf(" hex=");
            for (int i = 0; i < length; i++) printf("%02x", input[i]);
            success = length == 24 && input[0] == 0x3c;
        } else if (length < 0) printf(" error=%ls", hid_error(device));
        puts("");
    }
    hid_close(device);
    hid_exit();
    printf("RESULT: %s\n", success ? "native HID model reply confirmed without root" : "no native HID model reply");
    return success ? 0 : 1;
}
