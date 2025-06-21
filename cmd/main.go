package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"github.com/nagypeterjob/sock-vmnet/internal/network"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"inet.af/netaddr"
)

func main() {
	ctx := newCancelableContext()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	if err := run(ctx); err != nil {
		log.Error().Err(err).Msg("running network stack")
		os.Exit(1)
	}

	<-ctx.Done()
}

func run(ctx context.Context) error {
	var fd string
	var macAddr string
	var startAddr string
	var endAddr string
	var subnetMask string
	var debug bool

	flag.StringVar(&fd, "fd", "", "")
	flag.StringVar(&macAddr, "mac", "", "")
	flag.StringVar(&startAddr, "start-addr", "192.168.64.1", "")
	flag.StringVar(&endAddr, "end-addr", "192.168.64.255", "")
	flag.StringVar(&subnetMask, "subnet-mask", "255.255.255.0", "")
	flag.BoolVar(&debug, "debug", false, "")

	zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	flag.Parse()

	fdInt, err := strconv.Atoi(fd)
	if err != nil {
		return fmt.Errorf("parsing file descriptor: %w", err)
	}

	log.Debug().Msgf("VM MAC address: %s", macAddr)

	hardwareAddr, err := net.ParseMAC(macAddr)
	if err != nil {
		return fmt.Errorf("parsing provided MAC address: %w", err)
	}

	st, err := network.New(network.Params{
		Fd:           fdInt,
		HardwareAddr: hardwareAddr,
		StartAddr:    netaddr.MustParseIP(startAddr),
		EndAddr:      netaddr.MustParseIP(endAddr),
		SubnetMask:   netaddr.MustParseIP(subnetMask),
		Debug:        debug,
	})
	if err != nil {
		return fmt.Errorf("creating proxy: %w", err)
	}

	if err := st.Run(ctx, dropPrivileges); err != nil {
		return fmt.Errorf("running proxy: %w", err)
	}

	return nil
}

// exit on signal.
func newCancelableContext() context.Context {
	doneCh := make(chan os.Signal, 1)
	signal.Notify(doneCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, os.Interrupt)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-doneCh
		log.Info().Msg("signal received")
		cancel()
	}()

	return ctx
}

func dropPrivileges() error {
	// TODO:
	u, err := user.Lookup("peternagy")
	if err != nil {
		return fmt.Errorf("lookup user: %v", err)
	}

	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("parse uid: %v", err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("parse gid: %v", err)
	}

	if err := syscall.Setgid(gid); err != nil {
		return fmt.Errorf("setgid: %v", err)
	}
	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("setuid: %v", err)
	}
	return nil
}
