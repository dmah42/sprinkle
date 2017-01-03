// Package main defines a command line for interacting with a flock.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dominichamon/flock"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	pb "github.com/dominichamon/flock/proto"
)

var (
	cmd  = flag.String("cmd", "", "The command to run")
	ram  = flag.Uint64("ram", 0, "The amount of RAM to reserve for the command")
	wait = flag.Bool("wait", true, "Whether to wait for the command to complete")
	addr = flag.String("addr", "", "The multicast address to use for discovery")
	port = flag.Int("port", 9998, "The port to listen on for discovery")
)

type client struct {
	hostname string
	port     int
}

func discoverSheep(addr string, port int, ips chan<- string) error {
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

	go func() {
		tick := time.NewTicker(5 * time.Second)
		select {
		case <-tick.C:
			glog.Info("Discovery timeout")
			done = true
			tick.Stop()
		}
	}()

	go func() {
		for !done {
			b := make([]byte, 1024)
			c.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, err := c.Read(b)
			if err != nil {
				glog.Error(err)
				break
			}
			s := string(b[:n])

			glog.Infof("discovery ack %q [%d]", s, n)

			ips <- s
		}
		c.Close()
		close(ips)
	}()

	// Send out a ping.
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

	name, err := os.Hostname()
	if err != nil {
		return err
	}

	addrs, err := net.LookupHost(name)
	if err != nil {
		return err
	}

	glog.Info("Sending discovery ping on %s", udpaddr)

	pc, err := net.DialUDP("udp", nil, udpaddr)
	if err != nil {
		return err
	}
	defer pc.Close()

	_, err = pc.Write([]byte(net.JoinHostPort(addrs[0], fmt.Sprintf("%d", port))))
	return err
}

func bestSheep(ctx context.Context, ram uint64, ips <-chan string) *flock.Sheep {
	var sheep *flock.Sheep
	bestFreeRam := uint64(math.Inf(1))

	for ip := range ips {
		glog.Infof("Discovered sheep at %s", ip)

		host, port, err := net.SplitHostPort(ip)
		if err != nil {
			glog.Error(err)
			continue
		}

		p, err := strconv.ParseInt(port, 10, 32)
		if err != nil {
			glog.Error(err)
			continue
		}

		s, err := flock.NewSheep(host, int(p))
		if err != nil {
			glog.Error(err)
			continue
		}

		stat, err := s.Client.Status(ctx, &pb.StatusRequest{})
		if err != nil {
			glog.Errorf("failed to get status for %+v: %s", s, err)
		}
		glog.Infof("Status of %s [%s]: %+v", s.Id, ip, stat)

		if stat.FreeRam > ram {
			if sheep == nil || stat.FreeRam < bestFreeRam {
				sheep = s
				bestFreeRam = stat.FreeRam
			}
		}
	}
	return sheep
}

func main() {
	flag.Parse()

	ctx := context.Background()

	// Discover best sheep.
	ips := make(chan string)
	if err := discoverSheep(*addr, *port, ips); err != nil {
		glog.Exit(err)
	}

	sheep := bestSheep(ctx, *ram, ips)
	if sheep == nil {
		glog.Exit(fmt.Errorf("failed to find sheep"))
	}
	defer func() {
		if err := sheep.Close(); err != nil {
			glog.Exit(err)
		}
	}()

	glog.Infof("Best sheep %s", sheep.Id)

	// Run command.
	resp, err := sheep.Client.Run(ctx, &pb.RunRequest{
		Cmd: *cmd,
		Ram: *ram,
	})
	if err != nil {
		glog.Exit(err)
	}

	job := resp.JobId
	glog.Infof("Running %d on %s", job, sheep.Id)
	if *wait {
		done := false
		for !done {
			resp, err := sheep.Client.Job(ctx, &pb.JobRequest{Id: job})
			if err != nil {
				glog.Exit(err)
			}
			glog.Infof(".. %+v", resp)
			done = resp.Exited
		}

		stream, err := sheep.Client.Logs(ctx, &pb.LogsRequest{JobId: job})
		if err != nil {
			glog.Exit(err)
		}
		var stdout, stderr []string
		for {
			chunk, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				glog.Exit(err)
			}
			switch chunk.Type {
			case pb.LogType_STDOUT:
				stdout = append(stdout, chunk.Chunk)
			case pb.LogType_STDERR:
				stderr = append(stderr, chunk.Chunk)
			}
		}
		if len(stdout) != 0 {
			glog.Info(strings.Join(stdout, ""))
		}
		if len(stderr) != 0 {
			glog.Error(strings.Join(stderr, ""))
		}
	}
}
