package internal

import (
	"fmt"
	"net"

	"google.golang.org/grpc"

	pb "github.com/dominichamon/swarm/api/swarm"
)

type Worker struct {
	Id string

	conn   *grpc.ClientConn
	Client pb.WorkerClient
}

func (w *Worker) Close() error {
	return w.conn.Close()
}

func NewWorker(host string, port int) (*Worker, error) {
	conn, err := grpc.Dial(net.JoinHostPort(host, fmt.Sprintf("%d", port)), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	return &Worker{
		Id:     net.JoinHostPort(host, fmt.Sprintf("%d", port)),
		conn:   conn,
		Client: pb.NewWorkerClient(conn),
	}, nil
}
