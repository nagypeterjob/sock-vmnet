#import "vmnet.h"
#include <assert.h>

const int errCallback        = 3000;
const int errPacketCountZero = 4000;

int _vmnet_start(interface_ref *interface, uint64_t *max_packet_size, uint64_t *mtu_size,
  char* start_addr, char* end_addr, char* subnet_mask, uint32_t operation_mode, bool isolation, bool debug) {
  xpc_object_t interface_desc = xpc_dictionary_create(NULL, NULL, 0);

  xpc_dictionary_set_string(
    interface_desc,
    vmnet_start_address_key,
    start_addr
  );

  xpc_dictionary_set_string(
    interface_desc,
    vmnet_end_address_key,
    end_addr
  );

  xpc_dictionary_set_string(
    interface_desc,
    vmnet_subnet_mask_key,
    subnet_mask
  );

  xpc_dictionary_set_uint64(
    interface_desc,
    vmnet_operation_mode_key,
    operation_mode
  );

  xpc_dictionary_set_bool(
    interface_desc,
    vmnet_enable_isolation_key,
    isolation
  );

  // NOTE: explore further
  xpc_dictionary_set_bool(
    interface_desc,
    vmnet_enable_tso_key,
    false
  );

  // NOTE: explore further
  xpc_dictionary_set_bool(
    interface_desc,
    vmnet_enable_checksum_offload_key,
    false
  );

  dispatch_queue_t interface_start_queue =
    dispatch_queue_create("io.vmnet.start", DISPATCH_QUEUE_SERIAL);

  dispatch_semaphore_t interface_start_semaphore =
    dispatch_semaphore_create(0);

  __block interface_ref _interface;
  __block vmnet_return_t interface_status;
  __block uint64_t vmnet_max_packet_size = 0;
  __block uint64_t vmnet_mtu_size = 0;

  __block const char *vmnet_subnet_mask = NULL;
  __block const char *vmnet_dhcp_range_start = NULL;
  __block const char *vmnet_dhcp_range_end = NULL;

  _interface = vmnet_start_interface(
    interface_desc,
    interface_start_queue,
    ^(vmnet_return_t status, xpc_object_t interface_param) {
      interface_status = status;

      if (status != VMNET_SUCCESS || !interface_param) {
        dispatch_semaphore_signal(interface_start_semaphore);
        return;
      }

      vmnet_max_packet_size = xpc_dictionary_get_uint64(
        interface_param,
        vmnet_max_packet_size_key
      );

      vmnet_mtu_size = xpc_dictionary_get_uint64(
        interface_param,
        vmnet_mtu_key
      );

      if (debug) {
        vmnet_dhcp_range_start = strdup(xpc_dictionary_get_string(
          interface_param,
          vmnet_start_address_key
        ));

        vmnet_dhcp_range_end = strdup(xpc_dictionary_get_string(
          interface_param,
          vmnet_end_address_key
        ));

        vmnet_subnet_mask = strdup(xpc_dictionary_get_string(
          interface_param,
          vmnet_subnet_mask_key
        ));

        printf("/////////////////VMNET/////////////////\n");
        printf("/// Start address:   %s   ///\n", vmnet_dhcp_range_start);
        printf("/// End address:     %s ///\n", vmnet_dhcp_range_end);
        printf("/// Subnet mask:     %s  ///\n", vmnet_subnet_mask);
        printf("/// MTU:             %d           ///\n", (int)vmnet_mtu_size);
        printf("/// Max packet size: %d           ///\n", (int)vmnet_max_packet_size);
        printf("///////////////////////////////////////\n");

        free((char*)vmnet_dhcp_range_start);
        free((char*)vmnet_dhcp_range_end);
        free((char*)vmnet_subnet_mask);
      }

      dispatch_semaphore_signal(interface_start_semaphore);
  });

  dispatch_semaphore_wait(interface_start_semaphore, DISPATCH_TIME_FOREVER);

  if (interface_status != VMNET_SUCCESS || _interface == NULL) {
		return interface_status;
  }

  dispatch_release(interface_start_queue);
  xpc_release(interface_desc);

  *interface = _interface;
  *max_packet_size = vmnet_max_packet_size;
  *mtu_size = vmnet_mtu_size;

  dispatch_queue_t if_q = dispatch_queue_create("io.vmnet.packet.avilable", 0);

  // Setup callback, so we have an event driven way of reading packets instead of burning CPU cycles.
  vmnet_return_t event_callback_start = vmnet_interface_set_event_callback(
    _interface,
    VMNET_INTERFACE_PACKETS_AVAILABLE,
    if_q,
    ^(interface_event_t event_mask, xpc_object_t _Nonnull event) {
      if (event_mask != VMNET_INTERFACE_PACKETS_AVAILABLE) {
        printf("Unknown vmnet interface event 0x%08x\n", event_mask);
        return;
      }

      uint64_t packets_available = xpc_dictionary_get_uint64(
        event,
        vmnet_estimated_packets_available_key
      );
      packetsAvailable(event_mask, packets_available);
  });

  if (event_callback_start != VMNET_SUCCESS) {
    return errCallback;
  }

  return VMNET_SUCCESS;
}

int _vmnet_stop(interface_ref interface) {
  vmnet_interface_set_event_callback(interface, VMNET_INTERFACE_PACKETS_AVAILABLE, NULL, NULL);

  dispatch_queue_t stop_queue =
    dispatch_queue_create("io.vmnet.stop", DISPATCH_QUEUE_SERIAL);
  dispatch_semaphore_t stop_semaphore = dispatch_semaphore_create(0);

  vmnet_return_t status = vmnet_stop_interface(
    interface, 
    stop_queue,
    ^(vmnet_return_t status) {
      dispatch_semaphore_signal(stop_semaphore);
  });

  if (status == VMNET_SUCCESS) {
      dispatch_semaphore_wait(stop_semaphore, DISPATCH_TIME_FOREVER);
  }

  dispatch_release(stop_queue);
  return status;
}

int _vmnet_write(interface_ref interface, void *bytes, size_t bytes_size) {
  struct iovec packets_iovec = {
    .iov_base = bytes,
    .iov_len = bytes_size,
  };

  struct vmpktdesc packets = {
    .vm_pkt_size = bytes_size,
    .vm_pkt_iov = &packets_iovec,
    .vm_pkt_iovcnt = 1,
    .vm_flags = 0,
  };

  int packets_count = packets.vm_pkt_iovcnt;
  vmnet_return_t status = vmnet_write(interface, &packets, &packets_count);

  free(bytes);

  return status;
}

int _vmnet_read(interface_ref interface, uint64_t max_packet_size, void **bytes, size_t *bytes_size) {
  struct iovec packets_iovec = {
    .iov_base = malloc(max_packet_size),
    .iov_len = max_packet_size,
  };

  struct vmpktdesc packets = {
    .vm_pkt_size = max_packet_size,
    .vm_pkt_iov = &packets_iovec,
    .vm_pkt_iovcnt = 1,
    .vm_flags = 0,
  };

  int packets_count = 1;
  vmnet_return_t status = vmnet_read(interface, &packets, &packets_count);

  *bytes = packets.vm_pkt_iov->iov_base;
  *bytes_size = packets.vm_pkt_size;

  free(packets_iovec.iov_base);

  if (packets_count < 1) {
    return errPacketCountZero;
  }

  return status;
}
