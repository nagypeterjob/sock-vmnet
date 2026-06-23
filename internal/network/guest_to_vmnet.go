package network

import (
	"bytes"
	"context"
	"errors"
	"net"
	"syscall"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog/log"
	"inet.af/netaddr"
)

var broadcastIP = netaddr.IPv4(255, 255, 255, 255)

// Read packets coming from guest & forward them to the VMNET interface
// if they pass the filtering rules
func (s *Network) guestToVMNET(ctx context.Context, conn net.Conn) {
	buf := make([]byte, s.vmnet.MaxPacketSize)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			n, err := conn.Read(buf)
			if err != nil {
				switch {
				case errors.Is(err, net.ErrClosed):
					log.Error().Msg("socket is already closed for read")
					return
				case errors.Is(err, syscall.ENOBUFS):
					log.Error().Err(err).Msgf("read socket buffer is full")
					return
				default:
					log.Error().Err(err).Msgf("reading from")
					continue
				}
			}

			s.preparePacket(buf[:n])
		}
	}
}

func (s *Network) preparePacket(rawBytes []byte) {
	packet := gopacket.NewPacket(rawBytes, layers.LayerTypeEthernet, s.packetDecodeOptions)
	layer := packet.Layer(layers.LayerTypeEthernet)

	if eth, ok := layer.(*layers.Ethernet); ok {
		// It doesn't come from our VM
		if !bytes.Equal(eth.SrcMAC, s.HardwareAddr) {
			return
		}
	}

	if !s.allowedFromVM(&packet) {
		log.Debug().Msg("frame not allowed from VM")
		return
	}

	if _, err := s.vmnet.Write(rawBytes); err != nil {
		log.Error().Err(err).Msg("writing to vmnet")
	}
}

func (s *Network) allowedFromVM(packet *gopacket.Packet) bool {
	layer := (*packet).Layer(layers.LayerTypeIPv4)
	if ip, ok := layer.(*layers.IPv4); ok {
		if s.allowIPv4(packet, ip) {
			return true
		}
		// continue check
	}

	layer = (*packet).Layer(layers.LayerTypeARP)
	if arp, ok := layer.(*layers.ARP); ok {
		return s.allowARP(arp)
	}

	return false
}

func (s *Network) allowARP(arp *layers.ARP) bool {
	addr := netaddr.IPFrom4([4]byte(arp.SourceProtAddress))
	if s.dm.hasLeases() {
		if s.dm.validIPAddress(addr) {
			return true
		}
	} else if addr.IsUnspecified() {
		return true
	}

	return false
}

func (s *Network) allowIPv4(packet *gopacket.Packet, ipPkt *layers.IPv4) bool {
	// We already know the VM IP
	if s.dm.hasLeases() {
		addr := netaddr.IPFrom4([4]byte(ipPkt.SrcIP))
		if s.dm.validIPAddress(addr) && ipPkt.DstIP.IsGlobalUnicast() {
			return true
		}
	}

	if ipPkt.DstIP.Equal(s.gateway) {
		return true
	}

	if ipPkt.Protocol != layers.IPProtocolUDP {
		return false
	}

	layer := (*packet).Layer(layers.LayerTypeUDP)
	if layer == nil {
		return false
	}

	if pkt, ok := layer.(*layers.UDP); ok {
		return s.allowUDP(pkt, ipPkt)
	}
	return false
}

func (s *Network) allowUDP(pkt *layers.UDP, ipPkt *layers.IPv4) bool {
	destinationAddr := netaddr.IPFrom4([4]byte(ipPkt.DstIP))
	if validDNSRequest(pkt) && s.dm.validDNSTarget(destinationAddr) {
		return true
	}

	if validDHCPRequest(pkt) && destinationAddr == broadcastIP {
		return true
	}

	return false
}
