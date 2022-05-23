package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/golang/glog"
	"github.com/mackerelio/go-osstat/memory"
	"golang.org/x/net/context"

	pb "github.com/dominichamon/swarm/api/swarm"
)

var (
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
	// TODO: replace with reference to binary/job.. see golang/groupcache
	cmd            *exec.Cmd
	stdout, stderr string
	complete       bool
}

type workerServer struct {
	pb.UnimplementedWorkerServer
}

func ram() (uint64, uint64, error) {
	memory, err := memory.Get()
	if err != nil {
		return 0, 0, err
	}

	return memory.Total, memory.Free, nil
}

func (s *workerServer) Status(_ context.Context, _ *pb.StatusRequest) (*pb.StatusResponse, error) {
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

func (s *workerServer) Run(_ context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
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

	jobs.Lock()
	jobId += 1
	id := jobId
	jobs.jobs[id] = j
	jobs.Unlock()

	go func() {
		jobs.RLock()
		j := jobs.jobs[id]
		jobs.RUnlock()

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

		glog.Infof("Marking job %d as complete", id)
		j.complete = true

		jobs.Lock()
		jobs.jobs[id] = j
		jobs.Unlock()
	}()

	return &pb.RunResponse{JobId: id}, nil
}

func (s *workerServer) Job(_ context.Context, req *pb.JobRequest) (*pb.JobResponse, error) {
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
					Usec: int64(su.Utime.Usec),
				},
				Stime: &pb.Timeval{
					Sec:  su.Stime.Sec,
					Usec: int64(su.Stime.Usec),
				},
				Maxrss: su.Maxrss,
			}
		}
	}
	return resp, nil
}

func (s *workerServer) Jobs(_ context.Context, _ *pb.JobsRequest) (*pb.JobsResponse, error) {
	resp := &pb.JobsResponse{}
	jobs.RLock()
	defer jobs.RUnlock()

	resp.Id = make([]int64, len(jobs.jobs))
	i := 0
	for id := range jobs.jobs {
		resp.Id[i] = id
		i++
	}
	return resp, nil
}

func (s *workerServer) Logs(req *pb.LogsRequest, stream pb.Worker_LogsServer) error {
	var job job
	for {
		jobs.RLock()
		var ok bool
		job, ok = jobs.jobs[req.JobId]
		if !ok {
			return fmt.Errorf("job %d not found", req.JobId)
		}
		jobs.RUnlock()

		if job.complete {
			break
		}
		glog.Infof("Waiting for job %d to complete", req.JobId)
		time.Sleep(3 * time.Second)
	}

	if req.Type == pb.LogType_STDOUT || req.Type == pb.LogType_BOTH {
		// chunk up stdout and stream them.
		out := job.stdout
		glog.Infof("out %q", out)
		for _, s := range strings.Split(out, "\n") {
			err := stream.Send(&pb.LogsResponse{
				Type:  pb.LogType_STDOUT,
				Chunk: s,
			})
			if err != nil {
				return err
			}
		}
	}
	if req.Type == pb.LogType_STDERR || req.Type == pb.LogType_BOTH {
		// chunk up stderr and stream them.
		out := job.stderr
		glog.Infof("err %q", out)
		for _, s := range strings.Split(out, "\n") {
			err := stream.Send(&pb.LogsResponse{
				Type:  pb.LogType_STDOUT,
				Chunk: s,
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
