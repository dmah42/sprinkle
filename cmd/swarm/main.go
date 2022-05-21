// Package main defines a command line for interacting with a swarm.
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

	"github.com/dominichamon/swarm/internal"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	pb "github.com/dominichamon/swarm/api/swarm"
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

func bestWorker(ctx context.Context, ram uint64, addrs <-chan string) *internal.Worker {
	var worker *internal.Worker
	bestFreeRam := uint64(math.Inf(1))

	for addr := range addrs {
		glog.Infof("discovered worker at %s", addr)

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

		s, err := internal.NewWorker(host, int(p))
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
			if worker == nil || stat.FreeRam < bestFreeRam {
				worker = s
				bestFreeRam = stat.FreeRam
			}
		}
	}
	return worker
}

func main() {
	flag.Parse()

	ctx := context.Background()

	// Discover best worker.
	addrs := make(chan string)
	if err := internal.Ping(*addr, *port, addrs); err != nil {
		glog.Exit("failed to find workers: +v", err)
	}

	worker := bestWorker(ctx, *ram, addrs)
	if worker == nil {
		glog.Exit(fmt.Errorf("failed to identify best worker"))
	}
	defer func() {
		if err := worker.Close(); err != nil {
			glog.Exit(err)
		}
	}()

	glog.Infof("best worker found: %q", worker.Id)

	// Run command.
	resp, err := worker.Client.Run(ctx, &pb.RunRequest{
		Cmd: *cmd,
		Ram: *ram,
	})
	if err != nil {
		glog.Exit(err)
	}

	job := resp.JobId
	glog.Infof("running job %d on worker %q", job, worker.Id)
	if *wait {
		done := false
		for !done {
			resp, err := worker.Client.Job(ctx, &pb.JobRequest{Id: job})
			if err != nil {
				glog.Exit(err)
			}
			glog.Infof(".. %+v", resp)
			done = resp.Exited
		}

		stream, err := worker.Client.Logs(ctx, &pb.LogsRequest{JobId: job})
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
