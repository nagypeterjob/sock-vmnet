// nolint:errcheck,exhaustivestruct,exhaustruct,godot
package network

import (
	"context"
	"fmt"
	"net"

	"github.com/google/gopacket"
	"github.com/nagypeterjob/sock-vmnet/internal/vmnet"
	"github.com/rs/zerolog/log"
	"inet.af/netaddr"
)

// Params is a collection of parameters needed for vmnet
type Params struct {
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

// Network orchestrates the duplex socket communication
type Network struct {
	// Network parameters passed to vmnet
	Params
	// Manages dhcp communication
	dm dhcpManager

	// Gateway IP
	gateway net.IP

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
func New(p Params) (*Network, error) {
	return &Network{
		Params: p,
		// First IP of the range is reserved for the gateway
		gateway: net.ParseIP(p.StartAddr.String()),
		dm: dhcpManager{
			lease: lease{},
		},
		vmnet: vmnet.New(vmnet.Params{
			StartAddr:  p.StartAddr,
			EndAddr:    p.EndAddr,
			SubnetMask: p.SubnetMask,
			MacAddr:    p.HardwareAddr,
			Debug:      p.Debug,
		}),
		// Lazy && NoCopy should be the fastest mode with the least allocations
		packetDecodeOptions: gopacket.DecodeOptions{Lazy: true, NoCopy: true},
	}, nil
}

// Run the networking stack.
func (n *Network) Run(ctx context.Context, callback func() error) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// New FileConn from the socket's file descriptor
	// From this point we can Read/Write the socket as with any net.Conn impl.
	conn, err := fileConn(n.Fd)
	if err != nil {
		return fmt.Errorf("opening file connection: %w", err)
	}
	defer conn.Close()

	// Start vmnet interface
	if err := n.vmnet.Start(runCtx); err != nil {
		return fmt.Errorf("starting interface: %w", err)
	}

	if err := callback(); err != nil {
		return fmt.Errorf("callback failed: %w", err)
	}

	defer func() {
		log.Info().Msg("Stopping vmnet")
		n.vmnet.Stop()
	}()

	// read & write vmnet
	go n.VMNETToGuest(runCtx, conn)
	go n.guestToVMNET(runCtx, conn)

	<-runCtx.Done()

	return nil
}
