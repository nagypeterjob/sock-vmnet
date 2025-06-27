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
)

func (s *Network) VMNETToGuest(ctx context.Context, conn net.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case pkts, ok := <-s.vmnet.Event:
			if !ok {
				return
			}
			for _, pkt := range pkts {
				// TODO: bubble up errors?
				s.writeConn(conn, pkt)
			}
		}
	}
}

func (s *Network) writeConn(conn net.Conn, rawBytes []byte) {
	packet := gopacket.NewPacket(rawBytes, layers.LayerTypeEthernet, s.packetDecodeOptions)

	if !allowedFromHost(&packet) {
		log.Debug().Msg("frame not allowed from host")
		return
	}

	layer := packet.Layer(layers.LayerTypeEthernet)
	if eth, ok := layer.(*layers.Ethernet); ok {
		if bytes.Equal(eth.DstMAC, s.HardwareAddr) {
			s.dm.inspect(&packet, s.gateway)
		}
	}

	if _, err := conn.Write(rawBytes); err != nil {
		switch {
		case errors.Is(err, net.ErrClosed):
			log.Error().Msg("socket is already closed for write")
			return
		case errors.Is(err, syscall.ENOBUFS):
			log.Error().Msg("write socket buffer is full")
			return
		default:
			log.Error().Err(err).Msg("writing to connection")
		}
	}
}

func allowedFromHost(packet *gopacket.Packet) bool {
	// allow if ARP and IPv4 packets
	if (*packet).Layer(layers.LayerTypeARP) != nil ||
		(*packet).Layer(layers.LayerTypeIPv4) != nil {
		return true
	}

	return false
}
