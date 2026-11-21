// MIT: diagnostic prototype. No download/start/commit/reset commands are sent.
#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/hid/IOHIDManager.h>
#include <IOKit/hid/IOHIDKeys.h>
#include <libusb-1.0/libusb.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static unsigned char input[4096];
static int received;
static int64_t number(IOHIDDeviceRef device, CFStringRef key) {
    CFTypeRef value = IOHIDDeviceGetProperty(device, key);
    int64_t result = -1;
    if (value && CFGetTypeID(value) == CFNumberGetTypeID())
        CFNumberGetValue(value, kCFNumberSInt64Type, &result);
    return result;
}
static void report(void *ctx, IOReturn result, void *sender, IOHIDReportType type,
                   uint32_t id, uint8_t *data, CFIndex length) {
    (void)ctx; (void)sender;
    printf("input result=0x%08x type=%u report_id=%u length=%ld hex=", result, type, id, length);
    for (CFIndex n = 0; n < length; n++) printf("%02x", data[n]);
    puts("");
    if (result == kIOReturnSuccess && length == 24 && data[0] == 0x3c) received = 1;
}
static IOHIDManagerRef manager(void) {
    IOHIDManagerRef m = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    CFMutableDictionaryRef match = CFDictionaryCreateMutable(kCFAllocatorDefault, 0,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    int vendor = 0x7365, product = 0x616c;
    CFNumberRef v = CFNumberCreate(NULL, kCFNumberIntType, &vendor);
    CFNumberRef p = CFNumberCreate(NULL, kCFNumberIntType, &product);
    CFDictionarySetValue(match, CFSTR(kIOHIDVendorIDKey), v);
    CFDictionarySetValue(match, CFSTR(kIOHIDProductIDKey), p);
    IOHIDManagerSetDeviceMatching(m, match);
    CFRelease(v); CFRelease(p); CFRelease(match);
    IOHIDManagerScheduleWithRunLoop(m, CFRunLoopGetCurrent(), kCFRunLoopDefaultMode);
    // Enumerate without opening all matching devices through the manager.
    // The selected device is opened once below, with its own input callback.
    printf("IOHIDManager created for enumeration only\n");
    return m;
}
static int inspect_hid(IOHIDManagerRef m, int send_model_query) {
    CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0.2, false);
    CFSetRef set = IOHIDManagerCopyDevices(m);
    CFIndex count = set ? CFSetGetCount(set) : 0;
    printf("matching_hid_devices=%ld\n", count);
    if (count != 1) { if (set) CFRelease(set); return 0; }
    IOHIDDeviceRef d;
    CFSetGetValues(set, (const void **)&d);
    printf("usage_page=%lld usage=%lld max_input=%lld max_output=%lld\n",
        number(d, CFSTR(kIOHIDPrimaryUsagePageKey)), number(d, CFSTR(kIOHIDPrimaryUsageKey)),
        number(d, CFSTR(kIOHIDMaxInputReportSizeKey)), number(d, CFSTR(kIOHIDMaxOutputReportSizeKey)));
    CFTypeRef desc = IOHIDDeviceGetProperty(d, CFSTR(kIOHIDReportDescriptorKey));
    if (desc && CFGetTypeID(desc) == CFDataGetTypeID()) {
        printf("hid_report_descriptor=");
        const UInt8 *bytes = CFDataGetBytePtr(desc);
        for (CFIndex n = 0; n < CFDataGetLength(desc); n++) printf("%02x", bytes[n]);
        puts("");
    }
    if (!send_model_query) { CFRelease(set); return 0; }
    IOReturn result = IOHIDDeviceOpen(d, kIOHIDOptionsTypeNone);
    printf("IOHIDDeviceOpen=0x%08x\n", result);
    if (result != kIOReturnSuccess) { CFRelease(set); return 0; }
    IOHIDDeviceRegisterInputReportCallback(d, input, sizeof(input), report, NULL);
    CFStringRef mode = CFSTR("domfie.HIDProbe");
    IOHIDDeviceUnscheduleFromRunLoop(d, CFRunLoopGetCurrent(), kCFRunLoopDefaultMode);
    IOHIDDeviceScheduleWithRunLoop(d, CFRunLoopGetCurrent(), mode);
    unsigned char command[32] = {0};
    memcpy(command, "KinSealStudioKiss", 17);
    command[17] = 0x31; command[18] = 2;
    result = IOHIDDeviceSetReport(d, kIOHIDReportTypeOutput, 0, command, sizeof(command));
    printf("model_query_set_report=0x%08x bytes=32\n", result);
    if (result == kIOReturnSuccess) {
        for (int n = 0; n < 60 && !received; n++) CFRunLoopRunInMode(mode, 0.05, false);
    }
    if (!received && result == kIOReturnSuccess) {
        unsigned char padded[256] = {0};
        memcpy(padded, command, sizeof(command));
        result = IOHIDDeviceSetReport(d, kIOHIDReportTypeOutput, 0, padded, sizeof(padded));
        printf("model_query_padded_set_report=0x%08x bytes=256\n", result);
        if (result == kIOReturnSuccess) {
            for (int n = 0; n < 60 && !received; n++) CFRunLoopRunInMode(mode, 0.05, false);
        }
    }
    IOHIDDeviceUnscheduleFromRunLoop(d, CFRunLoopGetCurrent(), mode);
    IOHIDDeviceRegisterInputReportCallback(d, input, sizeof(input), NULL, NULL);
    IOHIDDeviceClose(d, kIOHIDOptionsTypeNone);
    CFRelease(set);
    return received;
}
int main(int argc, char **argv) {
    setvbuf(stdout, NULL, _IOLBF, 0);
    printf("uid=%u euid=%u\n", getuid(), geteuid());
    if (geteuid() == 0) { puts("This diagnostic must run without root."); return 2; }
    int configure = argc == 2 && strcmp(argv[1], "--configure") == 0;
    IOHIDManagerRef m = manager();
    if (inspect_hid(m, 1)) { puts("RESULT: native HID model reply confirmed without root"); return 0; }
    libusb_context *ctx = NULL;
    int code = libusb_init(&ctx);
    if (code) { printf("libusb_init=%s\n", libusb_error_name(code)); return 1; }
    libusb_device **list;
    ssize_t count = libusb_get_device_list(ctx, &list);
    libusb_device *target = NULL;
    int matches = 0;
    for (ssize_t n = 0; n < count; n++) {
        struct libusb_device_descriptor d;
        if (!libusb_get_device_descriptor(list[n], &d) && d.idVendor == 0x7365 && d.idProduct == 0x616c) {
            target = list[n]; matches++;
        }
    }
    printf("matching_usb_devices=%d\n", matches);
    if (matches != 1) return 1;
    struct libusb_config_descriptor *cfg = NULL;
    code = libusb_get_config_descriptor(target, 0, &cfg);
    printf("advertised_configuration=%s\n", libusb_error_name(code));
    if (!code) {
        printf("configuration_value=%u interfaces=%u\n", cfg->bConfigurationValue, cfg->bNumInterfaces);
        for (int n = 0; n < cfg->bNumInterfaces; n++) {
            for (int a = 0; a < cfg->interface[n].num_altsetting; a++) {
                const struct libusb_interface_descriptor *i = &cfg->interface[n].altsetting[a];
                printf("interface=%u alt=%u class=%u interface_extra=", i->bInterfaceNumber, i->bAlternateSetting, i->bInterfaceClass);
                for (int x = 0; x < i->extra_length; x++) printf("%02x", i->extra[x]);
                puts("");
            }
        }
    }
    libusb_device_handle *h = NULL;
    code = libusb_open(target, &h);
    printf("libusb_open=%s\n", libusb_error_name(code));
    if (!code) {
        int active = -1;
        code = libusb_get_configuration(h, &active);
        printf("current_configuration=%d result=%s\n", active, libusb_error_name(code));
        if (configure && !code && active == 0 && cfg) {
            code = libusb_set_configuration(h, cfg->bConfigurationValue);
            printf("select_configuration=%s\n", libusb_error_name(code));
            for (int n = 0; n < 20; n++) CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0.05, false);
        }
        unsigned char descriptor[4096];
        code = libusb_control_transfer(h, 0x81, LIBUSB_REQUEST_GET_DESCRIPTOR, 0x2200, 0,
            descriptor, sizeof(descriptor), 2000);
        if (code >= 0) {
            printf("raw_report_descriptor_length=%d hex=", code);
            for (int n = 0; n < code; n++) printf("%02x", descriptor[n]);
            puts("");
        } else printf("raw_report_descriptor_error=%s\n", libusb_error_name(code));
        int success = inspect_hid(m, 1);
        libusb_close(h);
        if (cfg) libusb_free_config_descriptor(cfg);
        libusb_free_device_list(list, 1);
        libusb_exit(ctx);
        IOHIDManagerClose(m, kIOHIDOptionsTypeNone);
        CFRelease(m);
        printf("RESULT: %s\n", success ? "native HID model reply confirmed without root" : "no native HID model reply");
        return success ? 0 : 1;
    }
    return 1;
}
