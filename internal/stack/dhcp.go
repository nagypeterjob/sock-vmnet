// nolint:exhaustive,godot
package stack

import (
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog/log"
	"inet.af/netaddr"
)

const (
	// Well known DNS port
	dnsPort = 53
	// https://www.unix.com/man-page/osx/8/bootpd/
	// The bootpd/dhcp server listens on this port
	dhcpListenPort = 67
	// The bootpd/dhcp server replies on this port
	dhcpBroadcastPort = 68
)

type lease struct {
	// IP address offered by dhcp
	addr netaddr.IP
	// List of DNS servers offered by dhcp
	dnsServers []netaddr.IP

	// The time of dhcp accept + lease time
	validUntil time.Time
}

type dhcpManager struct {
	// dhcp lease information
	lease lease

	m sync.Mutex
}

// Determine if packet is dhcp packet
func (d *dhcpManager) inspect(packet *gopacket.Packet, gateway netaddr.IP) {
	var layer gopacket.Layer

	// is it an ipv4 packet?
	if layer = (*packet).Layer(layers.LayerTypeIPv4); layer == nil {
		return
	}

	pkt, ok := layer.(*layers.IPv4)
	if !ok {
		return
	}

	// is the packet coming from the gateway?
	if pkt.SrcIP.String() != gateway.String() {
		return
	}

	// is it an UDP packet?
	if pkt.Protocol != layers.IPProtocolUDP {
		return
	}

	layer = (*packet).Layer(layers.LayerTypeUDP)
	if layer == nil {
		return
	}

	pktUDP, ok := layer.(*layers.UDP)
	if !ok {
		return
	}

	if !(validDHCPReply(pktUDP)) {
		return
	}

	d.parseDhcpLease(packet)
}

// Try to parse the following information from the packet:
//
// - VM ip addr
//
// - Lease time
//
// - DNS servers
func (d *dhcpManager) parseDhcpLease(packet *gopacket.Packet) {
	pkt := (*packet).Layer(layers.LayerTypeDHCPv4)
	dhcp, ok := pkt.(*layers.DHCPv4)
	if !ok {
		return
	}

	d.m.Lock()
	for _, opt := range dhcp.Options {
		if opt.Type == layers.DHCPOptDNS {
			d.lease.dnsServers = parseDNSAddresses(opt.Data)
		}

		if opt.Type == layers.DHCPOptLeaseTime {
			leaseTime := parseLeaseTimeBytes(opt.Data)
			d.lease.validUntil = time.Now().Add(time.Second * time.Duration(leaseTime))
		}

		switch layers.DHCPMsgType(opt.Data[0]) {
		case layers.DHCPMsgTypeOffer:
			log.Debug().Msgf("dhcp: offered IP address is: %s", dhcp.YourClientIP.String())
		case layers.DHCPMsgTypeAck:
			// parse the VM IP addr from the dchp ACK message
			d.lease.addr = netaddr.IPFrom4([4]byte(dhcp.YourClientIP.To4()))
		}
	}
	d.m.Unlock()
}

func (d *dhcpManager) validIPAddress(addr netaddr.IP) bool {
	return d.lease.addr == addr && time.Now().Before(d.lease.validUntil)
}

func (d *dhcpManager) validDNSTarget(destination netaddr.IP) bool {
	for _, ip := range d.lease.dnsServers {
		if destination == ip {
			return true
		}
	}
	return false
}

func (d *dhcpManager) hasLeases() bool {
	d.m.Lock()
	defer d.m.Unlock()
	return d.lease.addr != netaddr.IP{}
}

// taken from:
// https://github.com/google/gopacket/blob/32ee38206866f44a74a6033ec26aeeb474506804/layers/dhcpv4.go#L516C2-L517
func parseLeaseTimeBytes(data []byte) int {
	return int(uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3]))
}

func validDNSRequest(pkt *layers.UDP) bool {
	return pkt.DstPort == dnsPort
}

func validDHCPRequest(pkt *layers.UDP) bool {
	return pkt.SrcPort == dhcpBroadcastPort || pkt.DstPort == dhcpListenPort
}

func validDHCPReply(pkt *layers.UDP) bool {
	return pkt.SrcPort == dhcpListenPort || pkt.DstPort == dhcpBroadcastPort
}

func parseDNSAddresses(data []byte) []netaddr.IP {
	ips := make([]netaddr.IP, 0)
	num := len(data) / 4
	j := 0
	for i := 0; i < num; i++ {
		ips = append(ips, netaddr.IPv4(data[j], data[j+1], data[j+2], data[j+3]))
		j += 4
	}
	return ips
}
