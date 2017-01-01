// Package main defines a command line for interacting with a flock.
package main

import (
	"flag"
	"io"
	"math"
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
)

type client struct {
	hostname string
	port     int
}

func main() {
	flag.Parse()

	ctx := context.Background()

	// Discover best sheep.
	var sheep *flock.Sheep
	bestFreeRam := uint64(math.Inf(1))
	// for _, s := range discovered {

	s, err := flock.NewSheep("localhost", 5432)
	if err != nil {
		glog.Exit(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			glog.Exit(err)
		}
	}()

	stat, err := s.Client.Status(ctx, &pb.StatusRequest{})
	if err != nil {
		glog.Exitf("Failed to get status for %+v: %s", s, err)
	}
	glog.Infof("Status of %s: %+v", s.Id, stat)

	if stat.FreeRam > *ram {
		if sheep == nil || stat.FreeRam < bestFreeRam {
			sheep = s
			bestFreeRam = stat.FreeRam
		}
	}
	// }

	glog.Infof("Best sheep %s (%d)", sheep.Id, bestFreeRam)

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
