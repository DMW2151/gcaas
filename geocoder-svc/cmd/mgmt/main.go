package main

import (

	// standard lib
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	// internal
	srv "github.com/dmw2151/geocoder/geocoder-svc/internal"
	pb "github.com/dmw2151/geocoder/geocoder-svc/proto"

	// external
	redis "github.com/go-redis/redis/v8"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	// grpc - gcaas mgmt options (this service)
	serverListenAddr = flag.String("host", "0.0.0.0", "serverListenAddr (default '0.0.0.0') defines the server's listening address")
	serverPort       = flag.Int("port", 50052, "serverPort (default: 50051) defines the port to listen on")

	// redis options
	redisHost = flag.String("redis-host", "search", "host of the redis server to use as a FT engine")
	redisPort = flag.Int("redis-port", 6379, "host of the redis server to use as a FT engine")
	redisDB   = flag.Int("redis-db", 0, "db of the redis server to use as a FT engine")
)

// ManagementServer Specific Constants //
const (

	// serverMaxQueuedTransactions - during `InsertorReplaceAddressData`, maximum number of pipelined transactions to
	// allow before calling Pipe.Exec()
	serverMaxQueuedTransactions = 1024

	// serverInsertionJobMaxDuration - during `InsertorReplaceAddressData`, the max duration the server will allow a
	// connection to insert data to redis
	serverInsertionJobMaxDuration = time.Second * 180
)

// ManagementServer - server for management api
type ManagementServer struct {
	pb.UnimplementedManagementServer
	client *redis.Client
}

// InsertorReplaceAddressData - call is only used internally for managing the data of an index
func (s *ManagementServer) InsertorReplaceAddressData(stream pb.Management_InsertorReplaceAddressDataServer) (err error) {

	var startTime = time.Now()    // call on entry as proxy for use w. cobbled-together request logger
	var numQueuedTransactions int // current number of un-submitted addresses in redis exec pipeline
	var jobSuccess bool           // success flag for insertion request; returned as part of pb.IOResponse
	var totalObjectsWritten int   // total addresses "committed" to redis; returned as part of pb.IOResponse
	var respCode = codes.OK       // status code; returned as part of pb.IOResponse

	ctx, cancel := context.WithTimeout(context.Background(), serverInsertionJobMaxDuration)
	defer cancel()

	// defer calling a log command w. the request details, blegh...
	defer func() {
		reqLogger := log.WithFields(log.Fields{
			"stream.totalObjectsWritten": totalObjectsWritten,
			"stream.jobSuccess":          jobSuccess,
			"duration":                   -1 * float64(startTime.Sub(time.Now()).Microseconds()) / float64(1000),
			"method":                     "/geocoder.Geocoder/InsertorReplaceAddressData",
			"status":                     respCode.String(),
		})

		if (err == nil) || (err == io.EOF) {
			reqLogger.Info("data ingest job successful")
		} else {
			reqLogger.WithFields(log.Fields{
				"err": err,
			}).Error("data ingest job failed")
		}
	}()

	pipe := s.client.TxPipeline()

	for {
		// any non-EOF error from stream processing should throw && exit
		address, err := stream.Recv()

		if (err != nil) && (err != io.EOF) {
			log.WithFields(log.Fields{
				"numTransactions": numQueuedTransactions,
				"err":             err.Error(),
			}).Error("failed to read addresses from stream")
			respCode = codes.Internal
			return status.Error(respCode, err.Error())
		}

		// if the buffer is sufficiently full; then reset the buffer && execute the pipe commands
		if (numQueuedTransactions >= serverMaxQueuedTransactions) || (err == io.EOF) {

			// execute pipeline
			_, rerr := pipe.Exec(ctx)
			if rerr != nil {
				log.WithFields(log.Fields{
					"numTransactions": numQueuedTransactions,
					"err":             rerr.Error(),
				}).Error("failed to write transaction pipe")

				if ctx.Err() != nil {
					respCode = codes.DeadlineExceeded
					return status.Error(respCode, ctx.Err().Error())
				}

				respCode = codes.Internal
				return status.Error(respCode, rerr.Error())
			}

			// increment `totalObjectsWritten` counter && reset `numQueuedTransactions`
			totalObjectsWritten += numQueuedTransactions
			numQueuedTransactions = 0

			// on successful exit (io.EOF w. no prevailing errors), send an OK back to client
			if err == io.EOF {
				jobSuccess = true
				return stream.SendAndClose(
					&pb.IOResponse{
						Success:             jobSuccess,
						TotalObjectsWritten: int32(totalObjectsWritten),
					})
			}
		}

		// while below not full; add cmd to transaction pipeline buffer
		if numQueuedTransactions < serverMaxQueuedTransactions {
			numQueuedTransactions++
			pipe.Do(ctx, "HSET", fmt.Sprintf("address:%s", address.Id),
				"location", fmt.Sprintf("%.8f, %.8f", address.Location.Latitude, address.Location.Longitude),
				"composite_street_address", address.CompositeStreetAddress,
			)
		}
	}
	return status.Error(codes.Unknown, "unknown code path")
}

func init() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.SetLevel(log.InfoLevel)
}

func main() {
	flag.Parse()

	grpcServer := grpc.NewServer([]grpc.ServerOption{}...)
	managementServer := &ManagementServer{
		client: srv.MustRedisClient(
			context.Background(),
			&srv.RedisClientOptions{
				DB:   *redisDB,
				Host: *redisHost,
				Port: *redisPort,
			},
		),
	}

	// register && serve
	pb.RegisterManagementServer(grpcServer, managementServer)

	// listen on address and port from flags
	lis := srv.MustListener(serverListenAddr, serverPort)
	grpcServer.Serve(lis)
}
