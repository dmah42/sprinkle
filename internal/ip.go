package internal

import (
	"errors"
	"net"

	"github.com/golang/glog"
)

// ExternalIP returns the external IP address of the current machine,
// avoiding any loopback or down interfaces.
func ExternalIP() (net.IP, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			glog.Infof("skipping %q as it is down", iface.Name)
			continue
		}

		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil {
				continue
			}

			if ip.To4() == nil {
				glog.Infof("skipping %q for no IPv4 address", iface.Name)
				continue
			}
			return ip, nil
		}
	}

	return nil, errors.New("no network interfaces found")
}

