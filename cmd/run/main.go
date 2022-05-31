// Package main defines a command line for running a command against the workers
package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/dominichamon/swarm/internal"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	pb "github.com/dominichamon/swarm/api/swarm"
)

var (
	cmd       = flag.String("cmd", "", "The command to run")
	ram       = flag.Uint64("ram", 0, "The amount of RAM to reserve for the command")
	wait      = flag.Bool("wait", true, "Whether to wait for the command to complete")
	addr      = flag.String("addr", "239.192.0.1:9999", "The multicast address to use for discovery")
	port      = flag.Int("port", 9998, "The port to listen on for discovery")
	retries   = flag.Int("retries", 3, "Number of times to retry running the command")
	retryWait = flag.Duration("retry_wait", 10*time.Second, "time between retries")
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
				// Close out any worker we previously found
				// TODO: error checking
				if worker != nil {
					worker.Close()
				}
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

	var worker *internal.Worker
	var resp *pb.RunResponse
	var errs []error
	for i := 0; i < *retries; i++ {
		// Close worker from previous attempt
		if worker != nil {
			// TODO: error checking
			worker.Close()
		}
		worker = bestWorker(ctx, *ram, addrs)
		if worker == nil {
			errs = append(errs, fmt.Errorf("failed to identify best worker"))
			time.Sleep(*retryWait)
			continue
		}

		glog.Infof("best worker found: %q", worker.Id)

		// Run command.
		var err error
		resp, err = worker.Client.Run(ctx, &pb.RunRequest{
			Cmd: *cmd,
			Ram: *ram,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to run command: %s", err))
			time.Sleep(*retryWait)
			continue
		}
		break
	}

	if len(errs) != 0 {
		for e := range errs {
			glog.Errorln(e)
		}
		glog.Exit()
	}

	defer func() {
		if err := worker.Close(); err != nil {
			glog.Exit(err)
		}
	}()

	job := resp.JobId
	glog.Infof("running job %d on worker %q", job, worker.Id)
	if *wait {
		// no need to check on the job as the logs stream until the job
		// is complete.
		stream, err := worker.Client.Logs(ctx, &pb.LogsRequest{JobId: job})
		if err != nil {
			glog.Exit(err)
		}
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
				fmt.Fprint(os.Stdout, chunk.Chunk)
			case pb.LogType_STDERR:
				fmt.Fprint(os.Stderr, chunk.Chunk)
			}
		}
	}
}
