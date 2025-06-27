// nolint:gocritic,exhaustivestruct,exhaustruct,nosnakecase
package vmnet

// #cgo LDFLAGS: -framework vmnet
// #include "vmnet.h"
import "C"

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"unsafe"

	"github.com/rs/zerolog/log"
	"inet.af/netaddr"
)

var (
	errUnspecifiedFailure      = errors.New("vmnet: unspecified failure")
	errOutOfMemory             = errors.New("vmnet: out of memory")
	errInvalidArgument         = errors.New("vmnet: invalid argument provided")
	errSetupIncomplete         = errors.New("vmnet: interface setup is incomplete")
	errPermissionDenied        = errors.New("vmnet: permission denied. Is the process running as root?")
	errPacketSizeLargerThanMTU = errors.New("vmnet: larger packet size than MTU")
	errKernelBufferExhausted   = errors.New("vmnet: kernel buffer exhausted")
	errTooManyPackets          = errors.New("vmnet: too many packets")
	errSharingServiceBusy      = errors.New("vmnet: sharing service busy")
	errNotAuthorized           = errors.New("vmnet: not authorized")
	errNotWritten              = errors.New("vmnet: packet not written")
	errSetupCallback           = errors.New("vmnet: could not setup callback")
	errNoPackageRead           = errors.New("vmnet: no package read")
)

const successCode = 1000

var errCodesMap = map[int]error{
	1001: errUnspecifiedFailure,
	1002: errOutOfMemory,
	1003: errInvalidArgument,
	1004: errSetupIncomplete,
	1005: errPermissionDenied,
	1006: errPacketSizeLargerThanMTU,
	1007: errKernelBufferExhausted,
	1008: errTooManyPackets,
	1009: errSharingServiceBusy,
	1010: errNotAuthorized,
	2001: errNotWritten,
	3000: errSetupCallback,
	4000: errNoPackageRead,
}

func maptoErr(code int) error {
	err, ok := errCodesMap[code]
	if !ok {
		return errUnspecifiedFailure
	}
	return err
}

// batchSize is the maximum amount of packets we read
// from the VMNET interface at once.
// Each read/write call allows up to 200 packets to be read or written
// for a maximum of 256KB. Each packet written should be a complete ethernet frame.
const batchSize = 200

type OperationMode C.uint32_t

// https://developer.apple.com/documentation/vmnet/operating_modes_t
const (
	// Host: not implemented
	Host OperationMode = 1000
	// Shared good old NAT
	Shared OperationMode = 1001
	// Bridged: not implemented
	Bridged OperationMode = 1002
)

type IsolationMode bool

// If enabled, no VM <-> VM communication allowed
// https://developer.apple.com/documentation/vmnet/vmnet_enable_isolation_key
const (
	// NOTE: might want to let the user define it via flags
	Enabled  IsolationMode = true
	Disabled IsolationMode = false
)

// Wee need to pass this global variable through the C realm of vmnet,
// so that we can access fields & functions of the VMNet struct from packetsAvailable func.
//
// Read more: https://eli.thegreenplace.net/2019/passing-callbacks-and-pointers-to-cgo/
// We could use something like this instead: https://github.com/mattn/go-pointer
var vmnetPtr *VMNet

type Params struct {
	StartAddr  netaddr.IP
	EndAddr    netaddr.IP
	SubnetMask netaddr.IP
	MacAddr    net.HardwareAddr
	Debug      bool
}

type VMNet struct {
	// vmnet params
	Params

	// The maximum size of the packet that can be written to the interface.
	// This also defines the minimum size of the packet that needs to be passed
	// to the vmnet function for a successful read.
	MaxPacketSize int
	// The MTU to be configured on the virtual interface in the guest operating system.
	MTU int
	// By listening on VMNET_INTERFACE_PACKETS_AVAILABLE events, the registered callback
	// notifes us that the interface is readable. The read packes are being passed to the Even chan.
	// See packetsAvailable for more.
	Event chan [][]byte

	// CGO representation of the VMNet interface
	iface C.interface_ref
	// CGO representation of max packet size
	mps C.ulonglong
	// CGO representation of mtu
	mtu C.ulonglong
}

var BufferPool sync.Pool

func New(p Params) *VMNet {
	return &VMNet{
		Params: p,
		// I found the 100 buffer size to be optimal performance wise
		Event: make(chan [][]byte, 100),
	}
}

func (v *VMNet) Start(ctx context.Context) error {
	startAddr := C.CString(v.StartAddr.String())
	endAddr := C.CString(v.EndAddr.String())
	subnetMask := C.CString(v.SubnetMask.String())
	macAddr := C.CString(v.MacAddr.String())

	// Create the interface. From this point, ifconfig will show both bridge100 and vmnet<n> interfaces.
	errCode := C._vmnet_start(&v.iface, &v.mps, &v.mtu,
		startAddr, endAddr, subnetMask, macAddr,
		C.uint32_t(Shared), C.bool(Enabled), C.bool(v.Debug),
	)
	if errCode != successCode || v.iface == nil {
		fmt.Println(errCode)
		return maptoErr(int(errCode))
	}

	// Read and save actual max packet size and mtu values from the interface config
	v.MaxPacketSize = int(v.mps)
	v.MTU = int(v.mtu)

	// set the global pointer to the current state of self
	vmnetPtr = v
	BufferPool = sync.Pool{
		New: func() any {
			b := make([]byte, batchSize*v.MaxPacketSize)
			return &b
		},
	}

	C.free(unsafe.Pointer(startAddr))
	C.free(unsafe.Pointer(endAddr))
	C.free(unsafe.Pointer(subnetMask))
	C.free(unsafe.Pointer(macAddr))

	return nil
}

func (v *VMNet) Stop() error {
	defer close(v.Event)
	if errCode := C._vmnet_stop(v.iface); errCode != successCode {
		return maptoErr(int(errCode))
	}
	return nil
}

func (v *VMNet) read(nPkt int) ([][]byte, error) {
	buffer := BufferPool.Get().(*[]byte)
	if cap(*buffer) < batchSize*v.MaxPacketSize {
		tmp := make([]byte, batchSize*v.MaxPacketSize)
		buffer = &tmp
	}
	*buffer = (*buffer)[:batchSize*v.MaxPacketSize]
	cPktSizes := make([]C.size_t, nPkt)
	var cPktCount C.int

	if errCode := C._vmnet_read(
		v.iface,
		v.mps,
		unsafe.Pointer(&(*buffer)[0]),
		C.size_t(v.MaxPacketSize),
		C.int(nPkt),
		&cPktCount,
		(*C.size_t)(unsafe.Pointer(&cPktSizes[0])),
	); errCode != successCode {
		return nil, maptoErr(int(errCode))
	}
	n := int(cPktCount)
	// TODO: cache
	out := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		pkt := make([]byte, int(cPktSizes[i]))
		copy(pkt, (*buffer)[i*v.MaxPacketSize:i*v.MaxPacketSize+int(cPktSizes[i])])
		out = append(out, pkt)
	}
	BufferPool.Put(buffer)
	return out, nil
}

func (v *VMNet) Write(p []byte) (int, error) {
	cBytes := C.CBytes(p)
	if errCode := C._vmnet_write(v.iface, cBytes, C.ulong(len(p))); errCode != successCode {
		return 0, maptoErr(int(errCode))
	}
	C.free(cBytes)
	return len(p), nil
}

type EventType uint32

const (
	packetAvailableEvent EventType = 1 << 0
)

//export packetsAvailable
func packetsAvailable(eventType uint32, n uint64) {
	// VMNet tells us how many packages we can expect to be able to read from the interface.
	if EventType(eventType) != packetAvailableEvent {
		return
	}
	for remainder := int(n); remainder > 0; {
		// cap the number of puckets we read in a go
		batch := min(remainder, batchSize)
		packets, err := vmnetPtr.read(batch)
		if err != nil {
			log.Error().Err(err).Msg("reading vmnet")
			// go about our bussiness
		}
		if len(packets) == 0 {
			break
		}
		remainder -= len(packets)

		select {
		case vmnetPtr.Event <- packets:
		default:
			log.Warn().Err(err).Msg("nothing burger")
		}
	}
}
