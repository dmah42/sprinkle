// Package main defines a command line for interacting with a flock.
package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"

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

func bestSheep(ctx context.Context, ram uint64, addrs <-chan string) *flock.Sheep {
	var sheep *flock.Sheep
	bestFreeRam := uint64(math.Inf(1))

	for addr := range addrs {
		glog.Infof("Discovered sheep at %s", addr)

		host, port, err := net.SplitHostPort(addr)
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
		glog.Infof("Status of %s [%s]: %+v", s.Id, addr, stat)

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
	addrs := make(chan string)
	if err := flock.Ping(*addr, *port, addrs); err != nil {
		glog.Exit(err)
	}

	sheep := bestSheep(ctx, *ram, addrs)
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
			if len(chunk.Chunk) == 0 {
				continue
			}
			switch chunk.Type {
			case pb.LogType_STDOUT:
				stdout = append(stdout, chunk.Chunk)
			case pb.LogType_STDERR:
				stderr = append(stderr, chunk.Chunk)
			}
		}
		if len(stdout) != 0 {
			fmt.Fprintln(os.Stdout, strings.Join(stdout, "\n"))
		}
		if len(stderr) != 0 {
			fmt.Fprintln(os.Stderr, strings.Join(stderr, "\n"))
		}
	}
}
