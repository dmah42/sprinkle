package hive

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/golang/glog"
)

// Ping sends out a multicast message to the given address and port and sends any responses to the given channel.
func Ping(addr string, port int, addrs chan<- string) error {
	// Sanity checks
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

	// Listen first.
	laddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}

	c, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return err
	}

	glog.Infof("Discovery listening on %s", laddr)

	var done bool

	// Stop the discovery scan after 5 seconds.
	go func() {
		tick := time.NewTicker(5 * time.Second)
		select {
		case <-tick.C:
			glog.Info("Discovery timeout")
			done = true
			tick.Stop()
		}
	}()

	// Check for acks.
	go func() {
		for !done {
			b := make([]byte, 1024)
			c.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, err := c.Read(b)
			if err != nil {
				if !err.(net.Error).Timeout() {
					glog.Error(err)
				}
				break
			}
			s := string(b[:n])

			glog.Infof("Discovery ack %q [%d]", s, n)

			addrs <- s
		}
		c.Close()
		close(addrs)
	}()

	// Send out a ping.
	name, err := os.Hostname()
	if err != nil {
		return err
	}

	laddrs, err := net.LookupHost(name)
	if err != nil {
		return err
	}

	glog.Info("Sending discovery ping on ", udpaddr)

	pc, err := net.DialUDP("udp", nil, udpaddr)
	if err != nil {
		return err
	}
	defer pc.Close()

	_, err = pc.Write([]byte(net.JoinHostPort(laddrs[0], fmt.Sprintf("%d", port))))
	return err
}
