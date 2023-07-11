// nolint:errcheck,exhaustivestruct,exhaustruct,godot
package stack

import (
	"context"
	"fmt"
	"net"

	"github.com/google/gopacket"
	"github.com/nagypeterjob/sock-vmnet/internal/vmnet"
	"github.com/rs/zerolog/log"
	"inet.af/netaddr"
)

// NetworkParams is a collection of parameters needed for vmnet
type NetworkParams struct {
	// Socket file descriptor
	Fd int
	// The vm's MAC address provided by Virtualization.Framework
	HardwareAddr net.HardwareAddr
	// Enables debug logging
	Debug bool
	// First IP address of the subnet operated by macOS's built-in DHCP server.
	// The running vms get IP address assigned from the (StartAddr + 1) - EndAddr range.
	// The StartAddr will be the gateway address exclusively.
	// Default StartAddr is 192.168.64.1.
	StartAddr netaddr.IP
	// Last IP address of the subnet operated by macOS's built-in DHCP server.
	// Default StartAddr is 192.168.64.255.
	EndAddr netaddr.IP
	// The default ubnet mask is 255.255.255.0
	SubnetMask netaddr.IP
}

// Represents a dhcpd lease, e.g:
//
// $ cat /var/db/dhcpd_leases
//
//	{
//		name=virtualmachine
//		ip_address=192.168.64.2
//		hw_address=1,5e:8b:78:73:78:14
//		identifier=1,5e:8b:78:73:78:14
//		lease=0x64a5f04b
//	}

// Stack orchestrates the duplex socket communication
type Stack struct {
	// Network parameters passed to vmnet
	NetworkParams
	// Manages dhcp communication
	dm dhcpManager

	// Gateway IP
	gateway netaddr.IP

	// Represents the vmnet API
	vmnet *vmnet.VMNet

	// Store the DecodeOptions in Stack, and use it at multiple places
	// to avoid code duplication
	packetDecodeOptions gopacket.DecodeOptions
}

// NewNetwork creates a new Network.
//
// - NAT provided by vmnet
//
// - vmenet(n) interface
//
// - bridge100 interface
func NewNetwork(p NetworkParams) (*Stack, error) {
	// First IP of the range is reserved for the gateway
	gateway := p.StartAddr

	return &Stack{
		NetworkParams: p,
		gateway:       gateway,
		dm: dhcpManager{
			lease: lease{},
		},
		vmnet: vmnet.New(vmnet.Params{
			StartAddr:  p.StartAddr,
			EndAddr:    p.EndAddr,
			SubnetMask: p.SubnetMask,
			Debug:      p.Debug,
		}),
		// Lazy && NoCopy should be the fastest mode with the least allocations
		packetDecodeOptions: gopacket.DecodeOptions{Lazy: true, NoCopy: true},
	}, nil
}

// Run the networking stack.
func (s *Stack) Run(ctx context.Context) error {
	cntx, cancel := context.WithCancel(ctx)
	defer cancel()

	// New FileConn from the socket's file descriptor
	// From this point we can Read/Write the socket as with any net.Conn impl.
	conn, err := fileConn(s.Fd)
	if err != nil {
		return fmt.Errorf("opening file connection: %w", err)
	}
	defer conn.Close()

	// Start vmnet operations
	if err := s.vmnet.Start(); err != nil {
		return fmt.Errorf("starting interface: %w", err)
	}

	defer func() {
		log.Info().Msg("Stopping vmnet")
		s.vmnet.Stop()
	}()

	// read & write vmnet
	go s.read(cntx, conn)
	go s.write(cntx, conn)

	<-cntx.Done()

	return nil
}
