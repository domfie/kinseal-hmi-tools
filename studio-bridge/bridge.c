// Drop-in AMZUsb.dll for the seven cdecl exports used by KinSeal Studio 5.1.
// No vendor code: signatures/return conventions were recovered by static analysis.
#define WIN32_LEAN_AND_MEAN
#ifndef _WIN32_WINNT
#define _WIN32_WINNT 0x0601
#endif
#include <windows.h>
#include <setupapi.h>
#include <winusb.h>
#include <objbase.h>
#include <stdio.h>
#include <wchar.h>
#include "packet_write.h"

_Static_assert(sizeof(void *) == 4, "The Studio DLL must be x86, also on Windows ARM");
_Static_assert(sizeof(SP_DEVINFO_DATA) == 28, "SetupAPI ABI");
_Static_assert(sizeof(SP_DEVICE_INTERFACE_DETAIL_DATA_W) == 6, "SetupAPI detail ABI");

static SRWLOCK lock = SRWLOCK_INIT;
static HANDLE device_file = INVALID_HANDLE_VALUE;
static WINUSB_INTERFACE_HANDLE usb;
static unsigned packet_limit;

static void log_event(const char *event, int requested, int returned, DWORD error) {
    WCHAR path[MAX_PATH];
    DWORD len = GetTempPathW(MAX_PATH, path);
    if (!len || len + 32 >= MAX_PATH) return;
    wcscat(path, L"kinseal-studio-winusb.log");
    HANDLE file = CreateFileW(path, FILE_APPEND_DATA,
        FILE_SHARE_READ | FILE_SHARE_WRITE, NULL, OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
    if (file == INVALID_HANDLE_VALUE) return;
    SYSTEMTIME now;
    GetSystemTime(&now);
    char line[256];
    int n = snprintf(line, sizeof(line),
        "%04u-%02u-%02uT%02u:%02u:%02u.%03uZ pid=%lu bridge=0.1.0 %s requested=%d returned=%d packet_limit=%u error=%lu\r\n",
        now.wYear, now.wMonth, now.wDay, now.wHour, now.wMinute,
        now.wSecond, now.wMilliseconds, (unsigned long)GetCurrentProcessId(),
        event, requested, returned, packet_limit, (unsigned long)error);
    if (n > 0 && n < (int)sizeof(line)) {
        DWORD written;
        WriteFile(file, line, (DWORD)n, &written, NULL);
    }
    CloseHandle(file);
}

static void disconnect_locked(void) {
    if (usb) WinUsb_Free(usb);
    usb = NULL;
    if (device_file != INVALID_HANDLE_VALUE) CloseHandle(device_file);
    device_file = INVALID_HANDLE_VALUE;
    packet_limit = 0;
}

static BOOL set_policy(UCHAR pipe, ULONG kind, ULONG size, void *value) {
    return WinUsb_SetPipePolicy(usb, pipe, kind, size, value);
}

static BOOL open_path(const WCHAR *path) {
    device_file = CreateFileW(path, GENERIC_READ | GENERIC_WRITE,
        FILE_SHARE_READ | FILE_SHARE_WRITE, NULL, OPEN_EXISTING, FILE_FLAG_OVERLAPPED, NULL);
    if (device_file == INVALID_HANDLE_VALUE) return FALSE;
    if (!WinUsb_Initialize(device_file, &usb)) goto fail;
    USB_INTERFACE_DESCRIPTOR descriptor;
    if (!WinUsb_QueryInterfaceSettings(usb, 0, &descriptor)) goto fail;
    if (descriptor.bInterfaceNumber != 0) goto invalid;
    WINUSB_PIPE_INFORMATION out = {0}, in = {0};
    for (UCHAR n = 0; n < descriptor.bNumEndpoints; n++) {
        WINUSB_PIPE_INFORMATION pipe;
        if (!WinUsb_QueryPipe(usb, descriptor.bAlternateSetting, n, &pipe)) goto fail;
        if (pipe.PipeId == 0x01) out = pipe;
        if (pipe.PipeId == 0x81) in = pipe;
    }
    if (out.PipeId != 1 || in.PipeId != 0x81 || out.PipeType != in.PipeType ||
        (out.PipeType != UsbdPipeTypeBulk && out.PipeType != UsbdPipeTypeInterrupt)) goto invalid;
    if (out.PipeType == UsbdPipeTypeInterrupt) {
        if (!out.MaximumPacketSize) goto invalid;
        packet_limit = out.MaximumPacketSize;
    }
    ULONG write_timeout = 5000, read_timeout = 30000;
    UCHAR off = 0;
    if (!set_policy(1, PIPE_TRANSFER_TIMEOUT, sizeof(write_timeout), &write_timeout) ||
        !set_policy(0x81, PIPE_TRANSFER_TIMEOUT, sizeof(read_timeout), &read_timeout) ||
        !set_policy(1, SHORT_PACKET_TERMINATE, sizeof(off), &off) ||
        !set_policy(0x81, IGNORE_SHORT_PACKETS, sizeof(off), &off) ||
        !set_policy(0x81, ALLOW_PARTIAL_READS, sizeof(off), &off) ||
        !set_policy(0x81, AUTO_FLUSH, sizeof(off), &off)) goto fail;
    return TRUE;
invalid:
    SetLastError(ERROR_NOT_SUPPORTED);
fail: {
    DWORD error = GetLastError();
    disconnect_locked();
    SetLastError(error);
    return FALSE;
    }
}

// Restrict every enumerated interface to the selected PnP instance, including
// when using the generic USB interface GUID fallback.
static BOOL contains_instance(const WCHAR *path, const WCHAR *instance) {
    size_t length = wcslen(instance);
    for (; *path; path++) if (wcsncmp(path, instance, length) == 0) return TRUE;
    return FALSE;
}

static BOOL try_guid(const WCHAR *text, const WCHAR *instance_path) {
    GUID guid;
    if (CLSIDFromString(text, &guid) != S_OK) return FALSE;
    HDEVINFO list = SetupDiGetClassDevsW(&guid, NULL, NULL, DIGCF_PRESENT | DIGCF_DEVICEINTERFACE);
    if (list == INVALID_HANDLE_VALUE) return FALSE;
    BOOL connected = FALSE;
    for (DWORD n = 0; ; n++) {
        SP_DEVICE_INTERFACE_DATA data = {0};
        data.cbSize = sizeof(data);
        if (!SetupDiEnumDeviceInterfaces(list, NULL, &guid, n, &data)) break;
        DWORD size = 0;
        SetupDiGetDeviceInterfaceDetailW(list, &data, NULL, 0, &size, NULL);
        if (size < sizeof(SP_DEVICE_INTERFACE_DETAIL_DATA_W) || size > 65536) continue;
        SP_DEVICE_INTERFACE_DETAIL_DATA_W *detail = HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, size);
        if (!detail) break;
        detail->cbSize = sizeof(*detail);
        if (SetupDiGetDeviceInterfaceDetailW(list, &data, detail, size, NULL, NULL)) {
            // Matching includes a trailing #, so a neighboring instance cannot match.
            WCHAR lower[32768];
            size_t length = wcslen(detail->DevicePath);
            if (length < 32768) {
                wcscpy(lower, detail->DevicePath);
                CharLowerBuffW(lower, (DWORD)length);
                if (contains_instance(lower, instance_path)) connected = open_path(detail->DevicePath);
            }
        }
        HeapFree(GetProcessHeap(), 0, detail);
        if (connected) break;
    }
    SetupDiDestroyDeviceInfoList(list);
    return connected;
}

static BOOL try_registry_key(HKEY key, const WCHAR *instance) {
    const WCHAR *names[] = {L"DeviceInterfaceGUIDs", L"DeviceInterfaceGUID"};
    for (unsigned n = 0; n < 2; n++) {
        WCHAR values[2048] = {0};
        DWORD type = 0, size = sizeof(values) - 2 * sizeof(WCHAR);
        if (RegQueryValueExW(key, names[n], NULL, &type, (BYTE *)values, &size) != ERROR_SUCCESS ||
            (type != REG_MULTI_SZ && type != REG_SZ)) continue;
        for (const WCHAR *p = values; *p; p += wcslen(p) + 1) {
            if (try_guid(p, instance)) return TRUE;
        }
    }
    return FALSE;
}

static BOOL connect_locked(void) {
    HDEVINFO list = SetupDiGetClassDevsW(NULL, NULL, NULL, DIGCF_ALLCLASSES | DIGCF_PRESENT);
    if (list == INVALID_HANDLE_VALUE) return FALSE;
    SP_DEVINFO_DATA selected = {0};
    WCHAR instance[2048] = {0};
    unsigned matches = 0;
    for (DWORD n = 0; ; n++) {
        SP_DEVINFO_DATA data = {0};
        data.cbSize = sizeof(data);
        if (!SetupDiEnumDeviceInfo(list, n, &data)) break;
        WCHAR id[2048];
        if (!SetupDiGetDeviceInstanceIdW(list, &data, id, 2048, NULL)) continue;
        CharLowerBuffW(id, (DWORD)wcslen(id));
        if (wcsncmp(id, L"usb\\vid_7365&pid_616c\\", 22) != 0) continue;
        selected = data;
        if (wcslen(id) + 2 >= 2048) continue;
        wcscpy(instance, id);
        matches++;
    }
    BOOL connected = FALSE;
    if (matches != 1) {
        log_event(matches ? "multiple-devices" : "device-not-found", 0, 0, ERROR_NOT_FOUND);
        goto done;
    }
    WCHAR service[64] = {0};
    if (!SetupDiGetDeviceRegistryPropertyW(list, &selected, SPDRP_SERVICE, NULL,
            (BYTE *)service, sizeof(service), NULL) || _wcsicmp(service, L"WinUSB") != 0) {
        log_event("WinUSB-driver-required", 0, 0, ERROR_NOT_SUPPORTED);
        goto done;
    }
    for (WCHAR *p = instance; *p; p++) if (*p == L'\\') *p = L'#';
    wcscat(instance, L"#");
    HKEY key = SetupDiOpenDevRegKey(list, &selected, DICS_FLAG_GLOBAL, 0, DIREG_DEV, KEY_READ);
    if (key != INVALID_HANDLE_VALUE) {
        connected = try_registry_key(key, instance);
        HKEY parameters;
        if (!connected && RegOpenKeyExW(key, L"Device Parameters", 0, KEY_READ, &parameters) == ERROR_SUCCESS) {
            connected = try_registry_key(parameters, instance);
            RegCloseKey(parameters);
        }
        RegCloseKey(key);
    }
    if (!connected) connected = try_guid(L"{F5970E6C-EBEE-424F-9249-8FECBAAB028F}", instance);
    if (!connected) connected = try_guid(L"{A5DCBF10-6530-11D2-901F-00C04FB951ED}", instance);
done:
    SetupDiDestroyDeviceInfoList(list);
    return connected;
}

__declspec(dllexport) void __cdecl AmzUsb_init(void) {
    AcquireSRWLockExclusive(&lock);
    disconnect_locked();
    log_event("init", 0, 0, 0);
    ReleaseSRWLockExclusive(&lock);
}

__declspec(dllexport) int __cdecl AmzUsb_isconnected(void) {
    AcquireSRWLockShared(&lock);
    int result = usb != NULL;
    ReleaseSRWLockShared(&lock);
    return result;
}

__declspec(dllexport) void __cdecl AmzUsb_connect(void) {
    AcquireSRWLockExclusive(&lock);
    disconnect_locked();
    BOOL connected = connect_locked();
    log_event("connect", 0, connected, connected ? 0 : GetLastError());
    ReleaseSRWLockExclusive(&lock);
}

__declspec(dllexport) void __cdecl AmzUsb_disconnect(void) {
    AcquireSRWLockExclusive(&lock);
    disconnect_locked();
    log_event("disconnect", 0, 0, 0);
    ReleaseSRWLockExclusive(&lock);
}

__declspec(dllexport) int __cdecl AmzUsb_readRaw(unsigned char *data, int length) {
    AcquireSRWLockExclusive(&lock);
    ULONG count = 0;
    if (data && length > 0) ZeroMemory(data, (size_t)length);
    BOOL ok = usb && data && length > 0 && WinUsb_ReadPipe(usb, 0x81, data, (ULONG)length, &count, NULL);
    DWORD error = ok ? 0 : GetLastError();
    if (!usb) error = ERROR_INVALID_HANDLE;
    int result = (int)count;
    if (!ok) {
        // Preserve libusb-0.1's negative errno convention for the Studio caller.
        if (error == ERROR_SEM_TIMEOUT || error == ERROR_TIMEOUT) result = -116;
        else if (error == ERROR_DEVICE_NOT_CONNECTED || error == ERROR_INVALID_HANDLE) result = -19;
        else result = -5;
    }
    log_event("read", length, result, error);
    ReleaseSRWLockExclusive(&lock);
    return result;
}

static int write_one(void *context, const unsigned char *data, int length) {
    (void)context;
    ULONG count = 0;
    if (!WinUsb_WritePipe(usb, 1, (unsigned char *)data, (ULONG)length, &count, NULL)) return -1;
    return (int)count;
}

__declspec(dllexport) int __cdecl AmzUsb_writeRaw(const unsigned char *data, int length) {
    AcquireSRWLockExclusive(&lock);
    // The vendor ABI returns 1 on complete write, 0 on transfer error, -1 if disconnected.
    int result;
    if (!usb) result = -1;
    else if (length < 0 || (!data && length)) result = 0;
    else result = packet_write(data, length, (int)packet_limit, write_one, NULL);
    log_event("write", length, result, result == 1 ? 0 : GetLastError());
    ReleaseSRWLockExclusive(&lock);
    return result;
}

// The original export is a no-op, not a USB reset or flush.
__declspec(dllexport) void __cdecl AmzUsb_clear_outbuffer(void) {}
