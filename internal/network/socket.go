// nolint: wrapcheck,godot,deadcode,unused
package network

import (
	"net"
	"os"
)

func fileConn(fd int) (net.Conn, error) {
	f := os.NewFile(uintptr(fd), "")
	defer f.Close()

	return net.FileConn(f)
}
