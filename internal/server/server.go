package server

import (
	"context"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/streadway/amqp"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/proto"
	"log"
	"net"
	"net/http"
	chats "ssh-realtime/internal/proto"
	"sync"
	"time"
)

var (
	messagesSent = promauto.NewCounter(prometheus.CounterOpts{
		Name: "chat_messages_sent_total",
		Help: "The total number of messages sent",
	})
	activeConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "chat_active_connections",
		Help: "The number of active connections",
	})
)

type Server struct {
	chats.UnimplementedChatServiceServer
	mu           sync.Mutex
	rooms        map[string]map[string]chan *chats.ChatMessage
	rabbitMQConn *amqp.Connection
	rabbitMQChan *amqp.Channel
}

func NewServer(rabbitMQURL string) (*Server, error) {
	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}

	err = ch.ExchangeDeclare(
		"chat_messages", // name
		"fanout",        // type
		true,            // durable
		false,           // auto-deleted
		false,           // internal
		false,           // no-wait
		nil,             // arguments
	)
	if err != nil {
		return nil, err
	}

	return &Server{
		rooms:        make(map[string]map[string]chan *chats.ChatMessage),
		rabbitMQConn: conn,
		rabbitMQChan: ch,
	}, nil
}

func (s *Server) JoinRoom(req *chats.JoinRequest, stream chats.ChatService_JoinRoomServer) error {
	activeConnections.Inc()
	defer activeConnections.Dec()

	s.mu.Lock()
	if _, ok := s.rooms[req.RoomId]; !ok {
		s.rooms[req.RoomId] = make(map[string]chan *chats.ChatMessage)
	}
	ch := make(chan *chats.ChatMessage, 100)
	s.rooms[req.RoomId][req.UserId] = ch
	s.mu.Unlock()

	// RabbitMQ setup
	q, err := s.rabbitMQChan.QueueDeclare(
		"",    // name (let server generate a unique name)
		false, // durable
		true,  // delete when unused
		true,  // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		return err
	}

	err = s.rabbitMQChan.QueueBind(
		q.Name,          // queue name
		"",              // routing key
		"chat_messages", // exchange
		false,
		nil,
	)
	if err != nil {
		return err
	}

	msgs, err := s.rabbitMQChan.Consume(
		q.Name, // queue
		"",     // consumer
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	if err != nil {
		return err
	}

	go func() {
		for d := range msgs {
			var chatMsg chats.ChatMessage
			err := proto.Unmarshal(d.Body, &chatMsg)
			if err != nil {
				log.Printf("Error unmarshaling message: %v", err)
				continue
			}
			if chatMsg.RoomId == req.RoomId {
				if err := stream.Send(&chatMsg); err != nil {
					log.Printf("Error sending message to client: %v", err)
					return
				}
			}
		}
	}()

	for {
		select {
		case <-stream.Context().Done():
			s.mu.Lock()
			delete(s.rooms[req.RoomId], req.UserId)
			if len(s.rooms[req.RoomId]) == 0 {
				delete(s.rooms, req.RoomId)
			}
			s.mu.Unlock()
			return nil
		case msg := <-ch:
			if err := stream.Send(msg); err != nil {
				return err
			}
		}
	}
}

func (s *Server) SendMessage(ctx context.Context, msg *chats.ChatMessage) (*chats.SendResponse, error) {
	body, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}

	err = s.rabbitMQChan.Publish(
		"chat_messages", // exchange
		"",              // routing key
		false,           // mandatory
		false,           // immediate
		amqp.Publishing{
			ContentType: "application/protobuf",
			Body:        body,
		})
	if err != nil {
		return nil, err
	}

	messagesSent.Inc()
	return &chats.SendResponse{Success: true}, nil
}

func RunServer(address string, etcdEndpoints []string, rabbitMQURL string) error {
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	server, err := NewServer(rabbitMQURL)
	if err != nil {
		return err
	}

	s := grpc.NewServer()
	chats.RegisterChatServiceServer(s, server)

	// Register health service
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(s, healthServer)

	// Register with etcd
	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   etcdEndpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return err
	}
	defer etcdClient.Close()

	kv := clientv3.NewKV(etcdClient)
	lease := clientv3.NewLease(etcdClient)

	// Create a lease that expires after 10 seconds
	leaseResp, err := lease.Grant(context.Background(), 10)
	if err != nil {
		return err
	}

	// Register the server with etcd using the lease
	_, err = kv.Put(context.Background(), "/chat/servers/"+address, address, clientv3.WithLease(leaseResp.ID))
	if err != nil {
		return err
	}

	// Keep the lease alive
	keepAliveChan, err := lease.KeepAlive(context.Background(), leaseResp.ID)
	if err != nil {
		return err
	}

	go func() {
		for range keepAliveChan {
			// Lease keep-alive response
		}
	}()

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(":2112", nil)
	}()

	log.Printf("Server listening on %s", address)
	return s.Serve(lis)
}
