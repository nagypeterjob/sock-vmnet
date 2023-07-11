// nolint: wrapcheck,godot,deadcode,unused
package stack

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

var errSyscallConnAssertion = errors.New("asserting syscall.Conn")

func fileConn(fd int) (net.Conn, error) {
	f := os.NewFile(uintptr(fd), "")
	defer f.Close()

	return net.FileConn(f)
}

// not used atm
func socketFD(conn net.Conn) int {
	if con, ok := conn.(syscall.Conn); ok {
		raw, err := con.SyscallConn()
		if err != nil {
			return 0
		}
		sfd := 0
		_ = raw.Control(func(fd uintptr) {
			sfd = int(fd)
		})
		return sfd
	}
	return 0
}

// not used atm
func setBuf(conn net.Conn, ratio float64) error {
	sysConn, ok := conn.(syscall.Conn)
	if !ok {
		return errSyscallConnAssertion
	}

	raw, err := sysConn.SyscallConn()
	if err != nil {
		return fmt.Errorf("getting SyscallConn: %w", err)
	}

	var bufSize float64 = 1024 * 1024
	err = raw.Control(func(fd uintptr) {
		fdi := int(fd)
		if err := unix.SetsockoptInt(fdi, unix.SOL_SOCKET, unix.SO_SNDBUF, int(bufSize)); err != nil {
			log.Error().Err(err).Msg("set socket send buffer size failed")
		}

		if err := unix.SetsockoptInt(fdi, unix.SOL_SOCKET, unix.SO_RCVBUF, int(bufSize*ratio)); err != nil {
			log.Error().Err(err).Msg("set socket receive buffer size failed")
		}

		err = unix.SetsockoptInt(fdi, unix.SOL_SOCKET, unix.TCP_NODELAY, 1)
		if err != nil {
			log.Error().Err(err).Msg("setting nodelay")
		}

		if err := unix.SetNonblock(fdi, true); err != nil {
			log.Error().Err(err).Msg("setting noblock")
		}
	})
	if err != nil {
		return fmt.Errorf("accessing file descriptor: %w", err)
	}

	return nil
}
