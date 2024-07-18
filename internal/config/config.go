package config

type Config struct {
	ServerAddress string
	EtcdEndpoints []string
	RabbitMQURL   string
}

func NewConfig() *Config {
	return &Config{
		ServerAddress: ":50051",
		EtcdEndpoints: []string{"localhost:2379"},
		RabbitMQURL:   "amqp://guest:guest@localhost:5672/",
	}
}
