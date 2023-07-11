#ifndef vmnet_h
#define vmnet_h

#include <sys/uio.h>
#include <vmnet/vmnet.h>

int _vmnet_start(interface_ref *interface, uint64_t *max_packet_size, uint64_t *mtu_size,
    char* start_addr, char* end_addr, char* subnet_mask, uint32_t operation_mode, bool isolation, bool debug);
int _vmnet_stop(interface_ref interface);
int _vmnet_write(interface_ref interface, void *bytes, size_t bytes_size);
int _vmnet_read(interface_ref interface, uint64_t max_packet_size, void **bytes, size_t *bytes_size);
extern void packetsAvailable(uint32_t eventType, uint64_t packetCount);

#endif
