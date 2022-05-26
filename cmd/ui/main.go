// Package ui defines a UI for visualizing a swarm.
package main

import (
	"embed"
	"flag"
	"fmt"
	"html"
	"html/template"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/dominichamon/swarm/internal"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	pb "github.com/dominichamon/swarm/api/swarm"
)

var (
	port = flag.Int("port", 1248, "The port on which to listen for HTTP")
	poll = flag.Duration("poll", 5*time.Minute, "The time to wait between discovery attempts")

	addr = flag.String("addr", "239.192.0.1:9999", "The multicast address to use for discovery")
	dport = flag.Int("dport", 9997, "The port on which to listen for discovery")

	worker workerMap
	status statusMap
	jobs   jobsMap

	//go:embed index.html
	embedFS embed.FS
	indexTmpl *template.Template
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

func (m *workerMap) remove(s *internal.Worker) error {
	m.RLock()
	defer m.RUnlock()
	if _, ok := m.worker[s.Id]; !ok {
		return fmt.Errorf("worker %q not found", s.Id)
	}

	m.Lock()
	defer m.Unlock()
	if _, ok := m.worker[s.Id]; !ok {
		return fmt.Errorf("worker %q not found", s.Id)
	}
	delete(m.worker, s.Id)

	return nil
}

type statusMap struct {
	sync.RWMutex
	status map[string]*pb.StatusResponse
}

type jobsMap struct {
	sync.RWMutex
	// TODO: add job id by making the value a map.
	jobs map[string][]*pb.JobResponse
}

func init() {
	indexTmpl = template.Must(template.New("index.html").ParseFS(embedFS, "index.html"))

	worker.Lock()
	worker.worker = make(map[string]*internal.Worker)
	worker.Unlock()

	status.Lock()
	status.status = make(map[string]*pb.StatusResponse)
	status.Unlock()

	jobs.Lock()
	jobs.jobs = make(map[string][]*pb.JobResponse)
	jobs.Unlock()
}

func handleError(w http.ResponseWriter, code int, err error) {
	w.WriteHeader(code)
	fmt.Fprintf(w, "%q", html.EscapeString(err.Error()))
	glog.Error(err)
}

func Index(w http.ResponseWriter, req *http.Request) {
	status.RLock()
	defer status.RUnlock()

	jobs.RLock()
	defer jobs.RUnlock()

	data := struct {
		Status map[string]*pb.StatusResponse
		Jobs   map[string][]*pb.JobResponse
	}{status.status, jobs.jobs}

	if err := indexTmpl.Execute(w, data); err != nil {
		handleError(w, http.StatusInternalServerError, err)
		return
	}
}

func handleDiscoveryAcks(ctx context.Context, addrs <-chan string) {
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
			glog.Warning(err)
		}
		glog.Infof("Status of %s: %+v", s.Id, stat)
		status.Lock()
		status.status[s.Id] = stat
		status.Unlock()

		// TODO: remove old worker
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
			jrs := make([]*pb.JobResponse, len(jobsResp.Id))
			for i, id := range jobsResp.Id {
				j, err := s.Client.Job(ctx, &pb.JobRequest{Id: id})
				if err != nil {
					glog.Warningf("Failed to get job for %+v, %d: %s", s, id, err)
					continue
				}
				jrs[i] = j
			}
			jobs.Lock()
			jobs.jobs[s.Id] = jrs
			jobs.Unlock()
		}

		time.Sleep(1 * time.Minute)
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

	http.HandleFunc("/", Index)
	glog.Infof("listening on port %d", *port)
	glog.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
