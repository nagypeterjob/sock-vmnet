package stack

import (
	"context"
	"errors"
	"net"
	"syscall"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog/log"
)

func (s *Stack) read(ctx context.Context, conn net.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case bytes := <-s.vmnet.Event:
			s.writeConn(conn, bytes)
		}
	}
}

func (s *Stack) writeConn(conn net.Conn, rawBytes []byte) {
	packet := gopacket.NewPacket(rawBytes, layers.LayerTypeEthernet, s.packetDecodeOptions)

	layer := packet.Layer(layers.LayerTypeEthernet)
	if eth, ok := layer.(*layers.Ethernet); ok {
		if string(eth.DstMAC) == string(s.HardwareAddr) {
			s.dm.inspect(&packet, s.gateway)
		}
	}

	if !allowedFromHost(&packet) {
		log.Debug().Msg("frame not allowed from host")
		return
	}

	if _, err := conn.Write(rawBytes); err != nil {
		if errors.Is(err, net.ErrClosed) {
			log.Debug().Msg("socket is already closed")
			return
		}

		if errors.Is(err, syscall.ENOBUFS) {
			log.Debug().Msg("write socket buffer is full")
			return
		}

		log.Error().Err(err).Msg("writing to connection")
	}
}

func allowedFromHost(packet *gopacket.Packet) bool {
	var layer gopacket.Layer
	// allow if ipv4 packet
	layer = (*packet).Layer(layers.LayerTypeIPv4)
	if _, ok := layer.(*layers.IPv4); ok {
		return true
	}
	// allow if ARP packet
	layer = (*packet).Layer(layers.LayerTypeARP)
	if _, ok := layer.(*layers.ARP); ok {
		return true
	}
	return false
}
