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

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pb "github.com/dominichamon/flock/proto"
)

var (
	port = flag.Int("port", 5432, "The port on which to listen")

	jobId int64
	jobs  jobMap
)

type jobMap struct {
	sync.RWMutex
	jobs map[int64]job
}

func init() {
	jobs.Lock()
	jobs.jobs = make(map[int64]job)
	jobs.Unlock()
}

type job struct {
	start          time.Time
	cmd            *exec.Cmd
	stdout, stderr string
}

type sheepServer struct {
}

func ram() (uint64, uint64, error) {
	var si syscall.Sysinfo_t
	if err := syscall.Sysinfo(&si); err != nil {
		return 0, 0, err
	}
	return si.Totalram, si.Freeram, nil
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

	total, free, err := ram()
	if err != nil {
		return nil, err
	}

	return &pb.StatusResponse{
		Ip:       addrs[0],
		Hostname: name,
		TotalRam: total,
		FreeRam:  free,
	}, nil
}

func (s *sheepServer) Run(_ context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	// TODO: get available ram
	_, fr, err := ram()
	if err != nil {
		return nil, fmt.Errorf("failed to determine free RAM: %s", err)
	}
	if req.Ram > fr {
		return nil, fmt.Errorf("not enough RAM; %d vs %d", req.Ram, fr)
	}

	j := job{
		start: time.Now(),
	}

	scmd := strings.Fields(req.Cmd)
	j.cmd = exec.Command(scmd[0], scmd[1:]...)
	stdout, err := j.cmd.StdoutPipe()
	if err != nil {
		glog.Warningf("Unable to attach to stdout for %q: %s", req.Cmd, err)
	}
	stderr, err := j.cmd.StderrPipe()
	if err != nil {
		glog.Warningf("Unable to attach to stderr for %q: %s", req.Cmd, err)
	}
	glog.Infof("Running %q", req.Cmd)
	err = j.cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to run %q: %q", req.Cmd, err)
	}
	go func() {
		out, err := ioutil.ReadAll(stdout)
		if err != nil {
			glog.Error(err)
			j.stdout = fmt.Sprintf("[E] Failed to read stdout for %q: %s", req.Cmd, err)
		} else {
			j.stdout = string(out)
		}

		out, err = ioutil.ReadAll(stderr)
		if err != nil {
			glog.Error(err)
			j.stderr = fmt.Sprintf("[E] Failed to read stderr for %q: %s", req.Cmd, err)
		} else {
			j.stderr = string(out)
		}

		if err := j.cmd.Wait(); err != nil {
			fmt.Println(err)
		}
	}()

	jobs.Lock()
	jobId += 1
	id := jobId
	jobs.jobs[id] = j
	jobs.Unlock()

	return &pb.RunResponse{Id: id}, nil
}

func (s *sheepServer) Job(_ context.Context, req *pb.JobRequest) (*pb.JobResponse, error) {
	jobs.RLock()
	job := jobs.jobs[req.Id]
	jobs.RUnlock()

	resp := &pb.JobResponse{
		StartTime: job.start.Unix(),
	}
	if job.cmd.ProcessState != nil {
		resp.Exited = job.cmd.ProcessState.Exited()
		resp.Success = job.cmd.ProcessState.Success()

		su := job.cmd.ProcessState.SysUsage().(*syscall.Rusage)
		if su != nil {
			resp.Rusage = &pb.RUsage{
				Utime: &pb.Timeval{
					Sec:  su.Utime.Sec,
					Usec: su.Utime.Usec,
				},
				Stime: &pb.Timeval{
					Sec:  su.Stime.Sec,
					Usec: su.Stime.Usec,
				},
				Maxrss: su.Maxrss,
			}
		}
	}
	return resp, nil
}

func main() {
	flag.Parse()
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	s := grpc.NewServer()
	pb.RegisterSheepServer(s, &sheepServer{})
	glog.Infof("listening on %d", *port)
	s.Serve(l)
}
