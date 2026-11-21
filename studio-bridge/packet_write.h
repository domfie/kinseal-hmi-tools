#ifndef KINSEAL_PACKET_WRITE_H
#define KINSEAL_PACKET_WRITE_H

// A short or failed write is never retried, since delivery would be ambiguous.
typedef int (*packet_write_fn)(void *, const unsigned char *, int);
static int packet_write(const unsigned char *data, int length, int limit,
                        packet_write_fn write, void *context) {
    if (length < 0 || limit < 0) return 0;
    int offset = 0;
    while (offset < length) {
        int size = length - offset;
        if (limit && size > limit) size = limit;
        int n = write(context, data + offset, size);
        if (n != size) return 0;
        offset += n;
    }
    return 1;
}
#endif
