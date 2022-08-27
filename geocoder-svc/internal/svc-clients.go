package srv

import (
	// standard lib
	"context"
	"fmt"
	"net"
	"os"
	"time"

	// external
	redis "github.com/go-redis/redis/v8"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// onConnectRedisHandler - light wrapper func thst implements redis.onconnect
func onConnectRedisHandler(ctx context.Context, cn *redis.Conn) error {
	log.Info("new redis client connection established")
	return nil
}

// RedisClientOptions - options for connecting to the redis client
type RedisClientOptions struct {
	Host     string
	Port, DB int
}

// MustRedisClient - initialize a new redis client -> panic on err out
func MustRedisClient(ctx context.Context, r *RedisClientOptions) *redis.Client {

	// init client
	client := redis.NewClient(&redis.Options{
		Addr:            fmt.Sprintf("%s:%d", r.Host, r.Port),
		Password:        os.Getenv("REDISCLI_AUTH"),
		DB:              r.DB,
		MaxRetries:      5,                      // high retry count w. aggressive backoff - allow large datasets to load into mem on init
		MinRetryBackoff: time.Millisecond * 16,  // aggressive backoff (up from default of 8 ms)
		MaxRetryBackoff: time.Millisecond * 512, // aggressive backoff (up from default of 512 ms)
		OnConnect:       onConnectRedisHandler,
	})

	// ping the server to make sure connection is valid
	_, err := client.Ping(ctx).Result()
	if err != nil {
		log.WithFields(log.Fields{
			"server_addr": fmt.Sprintf("%s:%d", r.Host, r.Port),
			"error":       err,
			"redis_db":    r.DB,
		}).Panic("failed to connect to redis server")
	}
	return client
}

// MustRPCClient - creates a new RPC client or panics
func MustRPCClient(serverHost string, serverPort int) *grpc.ClientConn {
	var dialOptions = []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.Dial(fmt.Sprintf("%s:%d", serverHost, serverPort), dialOptions...)
	if err != nil {
		log.WithFields(log.Fields{
			"err":  err,
			"host": serverHost,
			"port": serverPort,
		}).Panic("failed to dial GRPC server")
	}
	return conn
}

func MustListener(addr *string, port *int) net.Listener {
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", *addr, *port))
	if err != nil {
		log.WithFields(log.Fields{
			"bind_address": *addr,
			"bind_port":    *port,
			"err":          err,
		}).Panic("failed to listen on address")
	}
	return lis
}

// MustSpacesClient - creates a connection to DigitalOcean spaces
func MustSpacesClient() *s3.S3 {

	key := os.Getenv("DO_SPACES_KEY")
	secret := os.Getenv("DO_SPACES_SECRET")

	newSession := session.Must(session.New(&aws.Config{
		Credentials: credentials.NewStaticCredentials(key, secret, ""),
		Endpoint:    aws.String(batchServerStorageEndPoint),
		Region:      aws.String("us-east-1"), // dummy region for the AWS client
	}), nil)

	s3Client := s3.New(newSession)
	return s3Client
}
