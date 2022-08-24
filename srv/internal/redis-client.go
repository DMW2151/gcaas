package srv

import (
	// standard lib
	"context"
	"fmt"
	"os"
	"time"

	// external
	redis "github.com/go-redis/redis/v8"
	log "github.com/sirupsen/logrus"
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
		MaxRetries:      5,                           // high retry count w. aggressive backoff - allow large datasets to load into mem on init
		MinRetryBackoff: time.Millisecond * 128,      // aggressive backoff (up from default of 8 ms)
		MaxRetryBackoff: time.Millisecond * 1024 * 4, // aggressive backoff (up from default of 512 ms)
		OnConnect:       onConnectRedisHandler,
	})

	// ping the server to make sure connection is valid
	_, err := client.Ping(ctx).Result()
	if err != nil {
		log.WithFields(log.Fields{
			"server_addr": fmt.Sprintf("%s:%d", r.Host, r.Port),
			"error":       err,
			"redisdb":     r.DB,
		}).Panic("failed to connect to redis server")
	}
	return client
}
