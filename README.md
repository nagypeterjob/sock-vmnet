<h1 align="center">sock-vmnet</h1>
<p align="center">
    One of the very few VZFileHandleNetworkDeviceAttachment implementations
</p>

# What does it solve?
There are multiple network modes available for Apple's Virtualization framework on Apple Silicon. `sock-vmnet` is a userspace VZFileHandleNetworkDeviceAttachment implementation in golang. Compared to VZNATNetworkDeviceAttachment network mode, `sock-vmnet` gives you full control over the traffic groing through the VM. `sock-vmnet` solves VZNATNetworkDeviceAttachment's ARP spoofing problem.

## What it doesn't solve?
`sock-vmnet` doesn't try to solve the dhcp exhaustion problem dhcpd/bootpd(8) has.

### Caveats
- `sock-vmnet` needs to run as root. As an alternative, you could ask for an [entitlement](https://developer.apple.com/documentation/bundleresources/entitlements/com_apple_vm_networking) from Apple.
- Altough it was load and performance tested thoroughly, `sock-vmnet` was never used in production.
- Neither `softnet` nor `sock-vmnet` can compete with the superior TCP network performance of `VZNATNetworkDeviceAttachment` (at least beased on my benchmark). I am not sure if there is a huge difference when it comes to real life workloads.

# Inspiration
`sock-vmnet` is heavily inspired by [softnet](https://github.com/cirruslabs/softnet). Softnet is a beautiful piece of software, and I am really glad that `cirruslabs` made it Open Source. Apple is famously gatekeeping, their documentations tend to be poor and secretive. Softnet is a beacon in the dark that provides valuable insights that Apple should have provided in the first place. `softnet` implies huge amounts of domain knowledge and experince with macOS internals, which I can only admire. The goal of this repository is to (more-or-less) mimic what `softnet` does while maintaining similar performance, just in golang.

## Other useful repositories
- [qemu-vmnet](https://github.com/alessiodionisi/qemu-vmnet)
- [3d0c/vmnet](https://github.com/3d0c/vmnet)
- [macos-vmnet](https://github.com/hamishcoleman/macos-vmnet)
- [qemu-devel](https://lists.gnu.org/archive/html/qemu-devel/2021-02/msg04637.html)
- [edigaryev/vmnet](https://github.com/edigaryev/vmnet)

# Benchmarks

## Methodology
```bash
# UDP
iperf3 -u -c <address> -p 5201 -bandwidth 10G
# TCP
iperf3 -c <address> -p 5201 -bandwidth 10G
```
## Comparison

### Network performance

| Solution | HOST->VM (TCP) | VM->HOST (TCP) | HOST->VM (UDP) | VM->HOST (UDP) |
| :------- | :------------: | :------------: | :------------: | :------------: |
| VZNATNetworkDeviceAttachment | 17.6 Gbits/sec :tada: | 20.5 Gbits/sec :tada: | 2.73 Gbits/sec (~0 loss) | 3.10 Gbits/sec (~30% loss)
| Softnet |  1.16 Gbits/sec | 3.64 Gbits/sec | 2.79 Gbits/sec (~0.2 loss) :tada: | 3.64 Gbits/sec (~20% loss) 
| sock-vmnet |  1.16 Gbits/sec | 2.73 Gbits/sec | 2.62 Gbits/sec (~0.003 loss) | 3.66 Gbits/sec (~19% loss) :tada:

### Resource allocation

| Solution | avg idle CPU | avg memory |
| :------- | :-----: | :--------: |
| Softnet | 0.3% | 3 MB :tada:
| sock-vmnet | 0.1% :tada: | 24.8 MB
# Usage

Launch `sock-vmnet` from your Virtualization.Framework hypervisor implementation as subprocess.
```bash
sock-vmnet 
    --fd=<fd> \
    --mac=<mac_addr> \
    [--start-addr=<addr>] \
    [--end-addr=<addr>] \
    [--subnet-mask=<addr>] \
    [--debug=<bool>]

```

`fd`: Create & configure a **SOCK_DGRAM** socketpair, then pass one of the fds  
`mac`: Unique MAC address of your VM  
`start-addr`: The starting address of the subnet range you want to assign from. **default**: 192.168.64.1  
`end-addr`: The last address of the subnet range you want to assign from. **default**: 192.168.64.255  
`subnet-mask`: Subnet mask for the assignable subnet range. **default**: 255.255.255.0  
`debug`: Debug logs. **default**: false