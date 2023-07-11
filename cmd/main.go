package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"

	"github.com/nagypeterjob/sock-vmnet/internal/stack"
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
	flag.BoolVar(&debug, "debug", true, "")

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
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

	st, err := stack.NewNetwork(stack.NetworkParams{
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

	if err := st.Run(ctx); err != nil {
		return fmt.Errorf("running proxy: %w", err)
	}

	return nil
}

// exit on signal.
func newCancelableContext() context.Context {
	doneCh := make(chan os.Signal, 1)
	signal.Notify(doneCh, os.Interrupt)

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	go func() {
		<-doneCh
		log.Info().Msg("signal received")
		cancel()
	}()

	return ctx
}
