package flock

import (
	"fmt"
	"net"

	"google.golang.org/grpc"

	pb "github.com/dominichamon/flock/proto"
)

type Sheep struct {
	Id string

	conn   *grpc.ClientConn
	Client pb.SheepClient
}

func (s *Sheep) Close() error {
	return s.conn.Close()
}

func NewSheep(host string, port int) (*Sheep, error) {
	conn, err := grpc.Dial(net.JoinHostPort(host, fmt.Sprintf("%d", port)), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	return &Sheep{
		Id:     net.JoinHostPort(host, fmt.Sprintf("%d", port)),
		conn:   conn,
		Client: pb.NewSheepClient(conn),
	}, nil
}
