// Package main defines a stubby service for running jobs.
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	pb "github.com/dominichamon/flock/proto"
)

var (
	port = flag.Int("port", 5432, "The port on which to listen")

	jobId  int64
	jobsMu sync.RWMutex
	jobs   map[int64]job
)

type job struct {
	start          time.Time
	cmd            *exec.Cmd
	stdout, stderr string
}

type sheepServer struct {
}

func (s *sheepServer) Status(_ context.Context, _ *pb.StatusRequest) (*pb.StatusResponse, error) {
	name, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	addrs, err := net.LookupHost(name)
	if err != nil {
		return nil, err
	}

	// TODO: use syscall.Sysinfo to get ram

	return &pb.StatusResponse{
		Ip:       addrs[0],
		Hostname: name,
	}, nil
}

func (s *sheepServer) Run(_ context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	// TODO: get available ram
	avail := int64(0)
	if req.Ram < avail {
		return nil, fmt.Errorf("not enough RAM; %d vs %d", avail, req.Ram)
	}

	j := job{
		start: time.Now(),
	}

	scmd := strings.Fields(req.Cmd)
	j.cmd = exec.Command(scmd[0], scmd[1:]...)
	stdout, err := j.cmd.StdoutPipe()
	if err != nil {
		fmt.Printf("Unable to attach to stdout for %q: %s", req.Cmd, err)
	}
	stderr, err := j.cmd.StderrPipe()
	if err != nil {
		fmt.Printf("Unable to attach to stderr for %q: %s", req.Cmd, err)
	}
	fmt.Printf("Running %q\n", req.Cmd)
	err = j.cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to run %q: %q", req.Cmd, err)
	}
	go func() {
		out, err := ioutil.ReadAll(stdout)
		if err != nil {
			fmt.Println(err)
			j.stdout = fmt.Sprintf("[E] Failed to read stdout for %q: %s", req.Cmd, err)
		} else {
			j.stdout = string(out)
		}

		out, err = ioutil.ReadAll(stderr)
		if err != nil {
			fmt.Println(err)
			j.stderr = fmt.Sprintf("[E] Failed to read stderr for %q: %s", req.Cmd, err)
		} else {
			j.stderr = string(out)
		}

		if err := j.cmd.Wait(); err != nil {
			fmt.Println(err)
		}
	}()

	jobsMu.Lock()
	jobId += 1
	id := jobId
	jobs[id] = j
	jobsMu.Unlock()

	return &pb.RunResponse{Id: id}, nil
}

func (s *sheepServer) Job(_ context.Context, req *pb.JobRequest) (*pb.JobResponse, error) {
	jobsMu.RLock()
	job := jobs[req.Id]
	jobsMu.RUnlock()

	su := job.cmd.ProcessState.SysUsage().(*syscall.Rusage)

	return &pb.JobResponse{
		StartTime: job.start.Unix(),
		Exited:    job.cmd.ProcessState.Exited(),
		Success:   job.cmd.ProcessState.Success(),
		Rusage: &pb.RUsage{
			Utime: &pb.Timeval{
				Sec:  su.Utime.Sec,
				Usec: su.Utime.Usec,
			},
			Stime: &pb.Timeval{
				Sec:  su.Stime.Sec,
				Usec: su.Stime.Usec,
			},
			Maxrss: su.Maxrss,
		},
	}, nil
}

func main() {
	flag.Parse()
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	s := grpc.NewServer()
	pb.RegisterSheepServer(s, &sheepServer{})
	fmt.Printf("listening on %d\n", *port)
	s.Serve(l)
}
