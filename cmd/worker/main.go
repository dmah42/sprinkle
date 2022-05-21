// Package worker defines a stubby service for running jobs.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/golang/glog"
	"google.golang.org/grpc"

	pb "github.com/dominichamon/swarm/api/swarm"
)

var (
	port = flag.Int("port", 5432, "The port on which to listen for RPC requests")
	addr = flag.String("addr", "", "The host/ip and port on which to listen for multicast discovery pings")
)

func multicastInterface() (*net.Interface, error) {
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

			glog.Infof("discovery ping %q [%d]", s, n)

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

			name, err := os.Hostname()
			if err != nil {
				glog.Error(err)
				break
			}

			addrs, err := net.LookupHost(name)
			if err != nil {
				glog.Error(err)
				break
			}

			_, err = rc.Write([]byte(net.JoinHostPort(addrs[0], fmt.Sprintf("%d", *port))))
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
		glog.Exit(err)
	}

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		glog.Exit(err)
	}
	s := grpc.NewServer()
	pb.RegisterWorkerServer(s, &workerServer{})
	glog.Infof("listening on %d", *port)
	s.Serve(l)
}
