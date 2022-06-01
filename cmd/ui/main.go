// Package ui defines a UI for visualizing a set of workers and the jobs running on them.
package main

import (
	"embed"
	"flag"
	"fmt"
	"html"
	"html/template"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/dominichamon/sprinkle/internal"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	pb "github.com/dominichamon/sprinkle/api/sprinkle"
)

var (
	port       = flag.Int("port", 1248, "The port on which to listen for HTTP")
	poll       = flag.Duration("poll", 1*time.Minute, "The time to wait between discovery attempts")
	statusPoll = flag.Duration("status_poll", 10*time.Second, "The time to wait between status updates")

	addr  = flag.String("addr", "239.192.0.1:9999", "The multicast address to use for discovery")
	dport = flag.Int("dport", 9997, "The port on which to listen for discovery")

	worker workerMap
	status statusMap
	jobs   jobsMap

	//go:embed index.html
	embedFS   embed.FS
	indexTmpl *template.Template

	funcMap = template.FuncMap{
		"toGB": func(bytes uint64) string {
			return fmt.Sprintf("%.3f", float64(bytes)/(1000*1000*1000))
		},
		"duration": func(start int64, end int64) time.Duration {
			if end == 0 {
				return 0
			}
			return time.Unix(end, 0).Sub(time.Unix(start, 0))
		},
	}
)

type workerMap struct {
	sync.RWMutex
	worker map[string]*internal.Worker
}

func (m *workerMap) add(s *internal.Worker) {
	m.Lock()
	m.worker[s.Id] = s
	m.Unlock()
}

func (m *workerMap) clear() {
	m.Lock()
	for k := range m.worker {
		delete(m.worker, k)
	}
	m.Unlock()
}

type statusMap struct {
	sync.RWMutex
	status map[string]*pb.StatusResponse
}

type jobsMap struct {
	sync.RWMutex
	jobs map[string]map[int64]*pb.JobResponse
}

func init() {
	indexTmpl = template.Must(
		template.New("index.html").Funcs(funcMap).ParseFS(embedFS, "index.html"))

	worker.Lock()
	worker.worker = make(map[string]*internal.Worker)
	worker.Unlock()

	status.Lock()
	status.status = make(map[string]*pb.StatusResponse)
	status.Unlock()

	jobs.Lock()
	jobs.jobs = make(map[string]map[int64]*pb.JobResponse)
	jobs.Unlock()
}

func handleError(w http.ResponseWriter, code int, err error) {
	w.WriteHeader(code)
	fmt.Fprintf(w, "%q", html.EscapeString(err.Error()))
	glog.Error(err)
}

func index(w http.ResponseWriter, req *http.Request) {
	status.RLock()
	defer status.RUnlock()

	jobs.RLock()
	defer jobs.RUnlock()

	data := struct {
		Status       map[string]*pb.StatusResponse
		ActiveJobs   map[string]map[int64]*pb.JobResponse
		InactiveJobs map[string]map[int64]*pb.JobResponse
	}{
		status.status,
		make(map[string]map[int64]*pb.JobResponse),
		make(map[string]map[int64]*pb.JobResponse),
	}

	for id, js := range jobs.jobs {
		data.ActiveJobs[id] = make(map[int64]*pb.JobResponse)
		data.InactiveJobs[id] = make(map[int64]*pb.JobResponse)
		for jid, job := range js {
			switch job.State {
			case pb.JobResponse_STATE_PENDING, pb.JobResponse_STATE_RUNNING:
				data.ActiveJobs[id][jid] = job
			case pb.JobResponse_STATE_UNKNOWN, pb.JobResponse_STATE_COMPLETE:
				data.InactiveJobs[id][jid] = job
			}
		}
	}

	if err := indexTmpl.Execute(w, data); err != nil {
		handleError(w, http.StatusInternalServerError, err)
		return
	}
}

func favIcon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "public, max-age=7776000")

	ex, err := os.Executable()
	if err != nil {
		http.Error(w, "unable to get working directory", http.StatusInternalServerError)
	}
	pwd := filepath.Dir(ex)

	glog.Infof("serving %q", path.Join(pwd, "favicon.ico"))
	http.ServeFile(w, r, path.Join(pwd, "favicon.ico"))
}

func logo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=3600")

	ex, err := os.Executable()
	if err != nil {
		http.Error(w, "unable to get working directory", http.StatusInternalServerError)
	}
	pwd := filepath.Dir(ex)

	glog.Infof("serving %q", path.Join(pwd, "logo.png"))
	http.ServeFile(w, r, path.Join(pwd, "logo.png"))
}

func handleDiscoveryAcks(ctx context.Context, addrs <-chan string) {
	worker.clear()
	for saddr := range addrs {
		glog.Infof("Discovered worker at %s", saddr)

		host, port, err := net.SplitHostPort(saddr)
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
			glog.Errorf("Failed to create new worker: %s", err)
			continue
		}

		glog.Infof("Connected to %+v", s)
		worker.add(s)

		stat, err := s.Client.Status(ctx, &pb.StatusRequest{})
		if err != nil {
			glog.Warningf("Removing %s: %s", s.Id, err)
			status.Lock()
			delete(status.status, s.Id)
			status.Unlock()
		}
		glog.Infof("Status of %s: %+v", s.Id, stat)
		status.Lock()
		status.status[s.Id] = stat
		status.Unlock()
	}
}

func updateWorkers(ctx context.Context) {
	for {
		worker.RLock()
		ss := make([]*internal.Worker, len(worker.worker))
		i := 0
		for _, s := range worker.worker {
			ss[i] = s
			i++
		}
		worker.RUnlock()

		for _, s := range ss {
			stat, err := s.Client.Status(ctx, &pb.StatusRequest{})
			if err != nil {
				glog.Warningf("Failed to get status for %+v: %s", s, err)
				continue
			}
			glog.Infof("Status of %s: %+v", s.Id, stat)
			status.Lock()
			status.status[s.Id] = stat
			status.Unlock()

			jobsResp, err := s.Client.Jobs(ctx, &pb.JobsRequest{})
			if err != nil {
				glog.Warningf("Failed to get jobs for %+v: %s", s, err)
				continue
			}
			glog.Infof("Jobs for %s: %+v", s.Id, jobsResp)
			jrs := make(map[int64]*pb.JobResponse)
			for _, id := range jobsResp.Id {
				j, err := s.Client.Job(ctx, &pb.JobRequest{Id: id})
				if err != nil {
					glog.Warningf("Failed to get job for %+v, %d: %s", s, id, err)
					continue
				}
				jrs[id] = j
			}
			jobs.Lock()
			jobs.jobs[s.Id] = jrs
			jobs.Unlock()
		}

		time.Sleep(*statusPoll)
	}
}

func main() {
	flag.Parse()

	ctx := context.Background()

	go func() {
		for {
			addrs := make(chan string)
			err := internal.Ping(*addr, *dport, addrs)
			if err != nil {
				glog.Error(err)
				goto sleep
			}
			handleDiscoveryAcks(ctx, addrs)
		sleep:
			time.Sleep(*poll)
		}
	}()
	go updateWorkers(ctx)

	http.HandleFunc("/", index)
	http.HandleFunc("/favicon.ico", favIcon)
	http.HandleFunc("/logo.png", logo)
	glog.Infof("listening on port %d", *port)
	glog.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
