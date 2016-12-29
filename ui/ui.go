// Package main defines a UI for visualizing flocks.
package main

import (
	"errors"
	"flag"
	"fmt"
	"html"
	"html/template"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pb "github.com/dominichamon/flock/proto"
)

var (
	port = flag.Int("port", 1248, "The port on which to listen")

	clients clientMap
)

type clientMap struct {
	sync.RWMutex
	clients map[string]*client
}

type client struct {
	Status *pb.StatusResponse
	Port   int

	conn *grpc.ClientConn
	stub pb.SheepClient
}

func (c client) close() error {
	return c.conn.Close()
}

func (c client) key() (string, error) {
	if c.Status == nil {
		return "", errors.New("No status available")
	}
	return net.JoinHostPort(c.Status.Hostname, fmt.Sprintf("%d", c.Port)), nil
}

func (c *client) updateStatus(ctx context.Context) error {
	status, err := c.stub.Status(ctx, &pb.StatusRequest{})
	if err != nil {
		return err
	}
	glog.Infof("Status of %+v: %+v", c, status)
	// TODO: store the status in a separate map?
	c.Status = status
	return nil
}

func newClient(ip string, port int) (*client, error) {
	c := &client{Port: port}
	conn, err := grpc.Dial(net.JoinHostPort(ip, fmt.Sprintf("%d", port)), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	c.conn = conn
	c.stub = pb.NewSheepClient(c.conn)
	return c, nil
}

func init() {
	clients.Lock()
	clients.clients = make(map[string]*client)
	clients.Unlock()
}

func handleError(w http.ResponseWriter, code int, err error) {
	w.WriteHeader(code)
	fmt.Fprintf(w, "%q", html.EscapeString(err.Error()))
	glog.Error(err)
}

func Index(w http.ResponseWriter, req *http.Request) {
	t, err := template.New("index").Parse(
		`<html><body>
		<table>
		<thead><th>IP</th><th>Host</th><th>Port</th><th>Total RAM</th><th>Free RAM</th></thead>
		{{range $_, $client := .}}
			<tr>
				{{if $client.Status}}
					<td>{{$client.Status.Ip}}</td>
					<td>{{$client.Status.Hostname}}</td>
				{{else}}
					<td>&lt;UNKNOWN&gt;</td>
					<td>&lt;UNKNOWN&gt;</td>
				{{end}}
				<td>{{$client.Port}}</td>

				{{if $client.Status}}
					<td>{{$client.Status.TotalRam}}</td>
					<td>{{$client.Status.FreeRam}}</td>
				{{end}}
			</tr>
		{{end}}
		</table>
		</body></html>`)
	if err != nil {
		handleError(w, http.StatusInternalServerError, err)
		return
	}

	clients.RLock()
	defer clients.RUnlock()
	if err = t.Execute(w, clients.clients); err != nil {
		handleError(w, http.StatusInternalServerError, err)
		return
	}
}

func discovery(ctx context.Context) {
	// TODO: discovery scan
	for {

		// TODO: add new clients

		c, err := newClient("localhost", 5432)
		if err != nil {
			glog.Errorf("Failed to create new client: %s", err)
			continue
		}

		glog.Infof("Connected to %+v", c)
		clients.Lock()
		c.updateStatus(ctx)
		k, err := c.key()
		if err != nil {
			glog.Error(err)
			continue
		}
		clients.clients[k] = c
		clients.Unlock()

		// TODO: remove old clients

		time.Sleep(5 * time.Minute)
	}
}

func status(ctx context.Context) {
	for {
		clients.Lock()

		for k, c := range clients.clients {
			if err := c.updateStatus(ctx); err != nil {
				glog.Warningf("Failed to get status for %s: %s", k, err)
				continue
			}
			glog.Infof("Status of %s: %+v", k, c.Status)
		}

		clients.Unlock()

		time.Sleep(1 * time.Minute)
	}
}

func main() {
	flag.Parse()

	ctx := context.Background()

	go discovery(ctx)
	go status(ctx)

	http.HandleFunc("/", Index)
	glog.Infof("listening on port %d", *port)
	glog.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
