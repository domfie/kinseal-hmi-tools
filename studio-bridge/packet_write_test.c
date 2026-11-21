#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "packet_write.h"

struct host {
    unsigned char *delivered;
    int offset, calls, fail_at, short_at;
};
static int send_packet(void *context, const unsigned char *data, int size) {
    struct host *h = context;
    h->calls++;
    if (h->calls == h->fail_at) return -1;
    if (h->calls == h->short_at) return size - 1;
    // QEMU reports success even when usbredirhost discards oversized interrupt OUT.
    if (size <= 256) {
        memcpy(h->delivered + h->offset, data, (size_t)size);
        h->offset += size;
    }
    return size;
}
int main(void) {
    const int sizes[] = {0, 32, 256, 257, 4998, 65536, 212104};
    unsigned char *data = malloc(212104), *out = malloc(212104);
    assert(data && out);
    for (int i = 0; i < 212104; i++) data[i] = (unsigned char)(i * 37);
    for (unsigned n = 0; n < sizeof(sizes) / sizeof(sizes[0]); n++) {
        struct host h = {.delivered = out};
        assert(packet_write(data, sizes[n], 256, send_packet, &h) == 1);
        assert(h.offset == sizes[n]);
        assert(memcmp(data, out, (size_t)sizes[n]) == 0);
        assert(h.calls == (sizes[n] + 255) / 256);
    }
    struct host h = {.delivered = out, .fail_at = 2};
    assert(packet_write(data, 1024, 256, send_packet, &h) == 0);
    assert(h.calls == 2 && h.offset == 256);
    h = (struct host){.delivered = out, .short_at = 2};
    assert(packet_write(data, 1024, 256, send_packet, &h) == 0);
    assert(h.calls == 2 && h.offset == 256);
    // Bulk calls keep their original shape; their host semantics differ.
    h = (struct host){.delivered = out};
    assert(packet_write(data, 1024, 0, send_packet, &h) == 1);
    assert(h.calls == 1);
    free(data);
    free(out);
    puts("Studio bridge packet delivery, ABI result and failure-boundary tests passed.");
    return 0;
}
