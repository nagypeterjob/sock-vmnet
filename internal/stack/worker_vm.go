package stack

import (
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

func (s *Stack) write(ctx context.Context, conn net.Conn) {
	bytes := make([]byte, s.vmnet.MaxPacketSize)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			n, err := conn.Read(bytes)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					log.Error().Msg("socket is already closed")
					return
				}

				if errors.Is(err, syscall.ENOBUFS) {
					log.Error().Err(err).Msgf("read socket buffer is full")
					return
				}

				log.Error().Err(err).Msgf("reading from")
				continue
			}

			s.preparePacket(bytes[:n])
		}
	}
}

func (s *Stack) preparePacket(rawBytes []byte) {
	packet := gopacket.NewPacket(rawBytes, layers.LayerTypeEthernet, s.packetDecodeOptions)
	layer := packet.Layer(layers.LayerTypeEthernet)

	if eth, ok := layer.(*layers.Ethernet); ok {
		// It doesn't come from our VM
		if string(eth.SrcMAC) != string(s.HardwareAddr) {
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

func (s *Stack) allowedFromVM(packet *gopacket.Packet) bool {
	var layer gopacket.Layer
	layer = (*packet).Layer(layers.LayerTypeIPv4)
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

func (s *Stack) allowARP(arp *layers.ARP) bool {
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

func (s *Stack) allowIPv4(packet *gopacket.Packet, ipPkt *layers.IPv4) bool {
	// We already know the VM IP
	if s.dm.hasLeases() {
		addr := netaddr.IPFrom4([4]byte(ipPkt.SrcIP))
		if s.dm.validIPAddress(addr) && ipPkt.DstIP.IsGlobalUnicast() {
			return true
		}
	}

	if ipPkt.DstIP.String() == s.gateway.String() {
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

func (s *Stack) allowUDP(pkt *layers.UDP, ipPkt *layers.IPv4) bool {
	destinationAddr := netaddr.IPFrom4([4]byte(ipPkt.DstIP))
	if validDNSRequest(pkt) && s.dm.validDNSTarget(destinationAddr) {
		return true
	}

	if validDHCPRequest(pkt) && destinationAddr == broadcastIP {
		return true
	}

	return false
}
