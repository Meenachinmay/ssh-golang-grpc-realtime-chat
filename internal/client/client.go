package client

import (
	"context"
	"github.com/cenkalti/backoff/v4"
	"github.com/sony/gobreaker"
	clientv3 "go.etcd.io/etcd/client/v3"
	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
	"io"
	"log"
	chats "ssh-realtime/internal/proto"
	"ssh-realtime/pkg/utils"
	"time"
)

type Client struct {
	conn      *grpc.ClientConn
	client    chats.ChatServiceClient
	cb        *gobreaker.CircuitBreaker
	sshConfig *ssh.ClientConfig
}

func NewClient(serverAddress string, etcdEndpoints []string, sshConfig *ssh.ClientConfig) (*Client, error) {
	var conn *grpc.ClientConn
	var err error

	if len(etcdEndpoints) > 0 {
		etcdClient, err := clientv3.New(clientv3.Config{
			Endpoints:   etcdEndpoints,
			DialTimeout: 5 * time.Second,
		})
		if err != nil {
			return nil, err
		}

		resolver.Register(&etcdResolver{client: etcdClient})

		conn, err = grpc.Dial(
			"etcd:///chat/servers/",
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
		)
	} else {
		conn, err = grpc.Dial(serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	if err != nil {
		return nil, err
	}

	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "ChatService",
		MaxRequests: 1000,
		Interval:    5 * time.Second,
		Timeout:     10 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 3 && failureRatio >= 0.6
		},
	})

	client := &Client{
		conn:      conn,
		client:    chats.NewChatServiceClient(conn),
		cb:        cb,
		sshConfig: sshConfig,
	}

	// Only set up SSH tunnel if sshConfig is provided
	if sshConfig != nil {
		go func() {
			err := utils.CreateSSHTunnel("localhost:50051", "server:22", "localhost:50051", sshConfig)
			if err != nil {
				log.Printf("Error creating SSH tunnel: %v", err)
			}
		}()
	}

	return client, nil
}

func (c *Client) Close() {
	c.conn.Close()
}

func (c *Client) JoinRoom(userId, roomId string) error {
	stream, err := c.client.JoinRoom(context.Background(), &chats.JoinRequest{
		UserId: userId,
		RoomId: roomId,
	})
	if err != nil {
		return err
	}

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		log.Printf("Received: %s", msg.Content)
	}

	return nil
}

func (c *Client) SendMessage(userId, roomId, content string) error {
	operation := func() error {
		_, err := c.cb.Execute(func() (interface{}, error) {
			return c.client.SendMessage(context.Background(), &chats.ChatMessage{
				UserId:  userId,
				RoomId:  roomId,
				Content: content,
			})
		})
		return err
	}

	return backoff.Retry(operation, backoff.NewExponentialBackOff())
}

type etcdResolver struct {
	client *clientv3.Client
}

func (r *etcdResolver) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	ctx, _ := context.WithCancel(context.Background())

	watch := r.client.Watch(ctx, "/chat/servers/", clientv3.WithPrefix())
	go func() {
		for response := range watch {
			for _, ev := range response.Events {
				switch ev.Type {
				case clientv3.EventTypePut:
					cc.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: string(ev.Kv.Value)}}})
				case clientv3.EventTypeDelete:
					cc.UpdateState(resolver.State{Addresses: []resolver.Address{}})
				}
			}
		}
	}()

	return &etcdResolver{client: r.client}, nil
}

func (r *etcdResolver) Scheme() string {
	return "etcd"
}

func (r *etcdResolver) ResolveNow(o resolver.ResolveNowOptions) {}

func (r *etcdResolver) Close() {}
