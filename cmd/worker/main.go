// Package worker defines a stubby service for running jobs.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net"

	"github.com/dominichamon/sprinkle/internal"
	"github.com/golang/glog"
	"google.golang.org/grpc"

	pb "github.com/dominichamon/sprinkle/api/sprinkle"
)

var (
	port  = flag.Int("port", 5432, "The port on which to listen for RPC requests")
	addr  = flag.String("addr", "239.192.0.1:9999", "The multicast address to use for discovery")
	iface = flag.String("iface", "", "The interface on which to listen for pings. Defaults to first that supports multicast if unset")
)

func multicastInterface() (*net.Interface, error) {
	if *iface != "" {
		ifi, err := net.InterfaceByName(*iface)
		if err != nil {
			return nil, err
		}
		if ifi.Flags&net.FlagMulticast == 0 {
			return nil, fmt.Errorf("iface %q does not support multicast", *iface)
		}
		return ifi, nil
	}

	// iface was not provided: search for the first multicast-supporting iface
	ifis, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, ifi := range ifis {
		if ifi.Flags&net.FlagMulticast != 0 {
			return &ifi, nil
		}
	}
	return nil, errors.New("no multicast interfaces found")
}

func multicastListen(addr string) error {
	if addr == "" {
		return errors.New("expected valid addr")
	}

	udpaddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}

	if !udpaddr.IP.IsMulticast() {
		return fmt.Errorf("%q is not multicast", addr)
	}

	ifi, err := multicastInterface()
	if err != nil {
		return err
	}

	glog.Infof("multicast listening on %s %s", ifi.Name, udpaddr)

	c, err := net.ListenMulticastUDP(udpaddr.Network(), ifi, udpaddr)
	if err != nil {
		return err
	}

	go func() {
		for {
			b := make([]byte, 1024)
			n, err := c.Read(b)
			if err != nil {
				glog.Error(err)
				break
			}
			s := string(b[:n])

			glog.Infof("discovery ping %s [%d]", s, n)

			// Reply!
			raddr, err := net.ResolveUDPAddr("udp", s)
			if err != nil {
				glog.Error(err)
				continue
			}

			rc, err := net.DialUDP("udp", nil, raddr)
			if err != nil {
				glog.Error(err)
				continue
			}
			defer rc.Close()

			ip, err := internal.ExternalIP()
			if err != nil {
				glog.Error(err)
				break
			}

			_, err = rc.Write([]byte(net.JoinHostPort(ip.String(), fmt.Sprintf("%d", *port))))
			if err != nil {
				glog.Error(err)
				continue
			}
		}
		c.Close()
	}()
	return nil
}

func main() {
	flag.Parse()

	if err := multicastListen(*addr); err != nil {
		glog.Exit("failed to listen for multicast: ", err)
	}

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		glog.Exit("failed to listen for job requests:", err)
	}
	glog.Infof("starting worker on port %d", *port)
	s := grpc.NewServer()
	pb.RegisterWorkerServer(s, &workerServer{})
	glog.Infof("listening on port %d", *port)
	s.Serve(l)
}
