package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dominichamon/sprinkle/internal"
	"github.com/golang/glog"
	"github.com/mackerelio/go-osstat/loadavg"
	"github.com/mackerelio/go-osstat/memory"
	"golang.org/x/net/context"

	pb "github.com/dominichamon/sprinkle/api/sprinkle"
)

var (
	jobId int64
	jobs  jobMap

	loadLimit = flag.Float64("load_limit", 5.0, "defines the maximum load the worker can be under before rejecting jobs")
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
	start time.Time
	end   time.Time
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

	return memory.Total, memory.Available, nil
}

func load() (float64, float64, error) {
	load, err := loadavg.Get()
	if err != nil {
		return 0, 0, err
	}
	return load.Loadavg1, load.Loadavg5, nil
}

func (s *workerServer) Status(_ context.Context, _ *pb.StatusRequest) (*pb.StatusResponse, error) {
	name, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	ip, err := internal.ExternalIP()
	if err != nil {
		return nil, err
	}

	total, avail, err := ram()
	if err != nil {
		return nil, err
	}

	_, load5, err := load()
	if err != nil {
		return nil, err
	}

	return &pb.StatusResponse{
		Ip:       ip.String(),
		Hostname: name,
		TotalRam: total,
		FreeRam:  avail,
		Load:     load5,
	}, nil
}

func (s *workerServer) Run(_ context.Context, req *pb.RunRequest) (*pb.RunResponse, error) {
	_, fr, err := ram()
	if err != nil {
		return nil, fmt.Errorf("failed to determine free RAM: %s", err)
	}
	if req.Ram > fr {
		return nil, fmt.Errorf("not enough available RAM; %d vs %d", req.Ram, fr)
	}
	_, load5, err := load()
	if err != nil {
		return nil, fmt.Errorf("unable to determine load: %s", err)
	}
	if load5 > *loadLimit {
		return nil, fmt.Errorf("under too high load: %.3f (limit: %.3f)", load5, *loadLimit)
	}

	// TODO: enqueue the job for later processing to limit jobs per worker
	// see: http://www.goldsborough.me/go/2020/12/06/12-24-24-non-blocking_parallelism_for_services_in_go/
	j := job{
		start: time.Now(),
	}

	scmd := []string{"sh", "-c", req.Cmd}
	glog.Infof("Running command %q with args %+v", scmd[0], scmd[1:])
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

		if err := j.cmd.Wait(); err != nil {
			fmt.Println(err)
		}

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

		glog.Infof("Marking job %d as complete", id)
		j.complete = true
		j.end = time.Now()

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
		State:     pb.JobResponse_STATE_UNKNOWN,
	}
	if job.cmd.ProcessState != nil {
		resp.Success = job.cmd.ProcessState.Success()

		if job.cmd.ProcessState.Exited() {
			resp.EndTime = job.end.Unix()
			resp.State = pb.JobResponse_STATE_COMPLETE
		} else {
			resp.State = pb.JobResponse_STATE_RUNNING
		}

		su := job.cmd.ProcessState.SysUsage().(*syscall.Rusage)
		if su != nil {
			resp.Rusage = &pb.RUsage{
				Utime: &pb.Timeval{
					Sec:  int64(su.Utime.Sec),
					Usec: int64(su.Utime.Usec),
				},
				Stime: &pb.Timeval{
					Sec:  int64(su.Stime.Sec),
					Usec: int64(su.Stime.Usec),
				},
				Maxrss: int64(su.Maxrss),
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

// streamLogs chunks up the `logs` of type `t`, and streams to `stream`.
func streamLogs(stream pb.Worker_LogsServer, t pb.LogType, logs string) error {
	glog.Infof("chunking %q", logs)
	for _, s := range strings.Split(logs, "\n") {
		err := stream.Send(&pb.LogsResponse{
			Type:  t,
			Chunk: s + "\n",
		})
		if err != nil {
			return err
		}
	}
	return nil
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
		// TODO: don't wait for the job to complete before streaming
		// the logs.
		glog.Infof("Waiting for job %d to complete", req.JobId)
		time.Sleep(3 * time.Second)
	}

	if req.Type == pb.LogType_STDOUT || req.Type == pb.LogType_BOTH {
		if err := streamLogs(stream, pb.LogType_STDOUT, job.stdout); err != nil {
			return err
		}
	}
	if req.Type == pb.LogType_STDERR || req.Type == pb.LogType_BOTH {
		if err := streamLogs(stream, pb.LogType_STDERR, job.stderr); err != nil {
			return err
		}
	}
	return nil
}
