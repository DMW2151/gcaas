package main

import (

	// standard lib

	"context"
	"flag"
	"fmt"

	"time"

	// internal
	srv "github.com/dmw2151/geocoder/geocoder-svc/internal"
	pb "github.com/dmw2151/geocoder/geocoder-svc/proto"

	// external
	redis "github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/aws/aws-sdk-go/service/s3"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	// batch service options (this service)
	serverListenAddr = flag.String("host", "0.0.0.0", "serverListenAddr (default '0.0.0.0') defines the server's listening address")
	serverPort       = flag.Int("port", 50053, "serverPort (default: 50052) defines the port to listen on")

	// redis options
	redisCacheHost = flag.String("redis-host", "batch-cache", "host of the redis server to use as a response cache")
	redisCachePort = flag.Int("redis-port", 6379, "host of the redis server to use as a response cache")
	redisCacheDB   = flag.Int("redis-db", 0, "db of the redis server to use as a response cache")

	// queue parameters
	pubsubHost = flag.String("pubsub-host", "pubsub", "...")
	pubsubPort = flag.Int("pubsub-port", 6379, "...")
	pubsubDB   = flag.Int("pubsub-db", 0, "...")
)

// GeocoderServer - server API for Geocoder service
type BatchServer struct {
	pb.UnimplementedBatchServer
	cacheClient  *redis.Client
	pubsubClient *redis.Client
	spacesClient *s3.S3
}

// Listen - the batch server listens with one client (pubsub) and writes to cache
// with the other
func (s *BatchServer) Listen(ctx context.Context, topic string) {
	sub := s.pubsubClient.Subscribe(ctx, topic)
	defer func() {
		log.Infof("exit from pub/sub channel: %s", topic)
		sub.Unsubscribe(s.pubsubClient.Context(), topic)
	}()

	channel := sub.Channel()

	var r pb.BatchStatusResponse
	var err error

	// marshall msg from the pub/sub channel and write to cache
	for msg := range channel {
		log.Infof("batchserver.listener recv msg on %s", topic)

		if err := proto.Unmarshal([]byte(msg.Payload), &r); err != nil {
			log.WithFields(log.Fields{
				"err": err,
				"op":  "batchserver.listener",
			}).Panicf("failed to read message from pubsub: %s", s)
		}

		_, err = s.cacheClient.Do(
			ctx, "HSET", r.Id, "status", r.Status.String(), "download_path", r.DownloadPath, "update_time", time.Now(),
		).Result()

		if err != nil {
			log.WithFields(log.Fields{
				"err":                err,
				"batch.id":           r.Id,
				"batch.status":       r.Status,
				"batch.downloadpath": r.DownloadPath,
				"op":                 "batchserver.listener",
			}).Error("failed to set status on batch-cache")
			break
		}

		log.WithFields(log.Fields{
			"batch.id":           r.Id,
			"batch.status":       r.Status,
			"batch.downloadpath": r.DownloadPath,
			"op":                 "batchserver.listener",
		}).Info("set status on batch-cache")
	}
}

// CreateBatch - creates a new batch and sends an event to the queue
func (s *BatchServer) CreateBatch(ctx context.Context, req *pb.CreateBatchRequest) (*pb.BatchStatusResponse, error) {

	var startTime = time.Now()               // call on entry as proxy for use w. cobbled-together request logger
	var respCode = codes.OK                  // status code; returned as part of pb.IOResponse
	var err error                            // error; returned as part of pb.IOResponse
	var batchRequestId = uuid.New().String() // create a new uuid for the request

	reqLogger := log.WithFields(log.Fields{
		"request.size":   len(req.Points) + len(req.Addresses),
		"request.method": req.Method,
		"method":         "/geocoder.Batch/CreateBatch",
		"batch.id":       batchRequestId,
	})

	defer func() {
		if respCode == codes.OK {
			reqLogger.WithFields(log.Fields{
				"duration": -1 * float64(startTime.Sub(time.Now()).Microseconds()) / float64(1000),
				"status":   respCode.String(),
			}).Info("create batch request successful")
		} else {
			reqLogger.WithFields(log.Fields{
				"err":      err,
				"duration": -1 * float64(startTime.Sub(time.Now()).Microseconds()) / float64(1000),
				"status":   respCode.String(),
			}).Error("create batch request failed")
		}
	}()

	// first thing we do is mark accepted and tell the client the request was
	// accepted unless the cache rejected it upfront...
	_, err = s.cacheClient.Do(
		ctx, "HSET", batchRequestId, "status", pb.BatchGeocodeStatus_ACCEPTED.String(), "update_time", time.Now(),
	).Result()
	if err != nil {
		respCode = codes.Unavailable // transient failure - batch status cache unavailable
		return &pb.BatchStatusResponse{
			Id:         batchRequestId,
			Status:     pb.BatchGeocodeStatus_REJECTED, // Rejected
			UpdateTime: timestamppb.New(time.Now()),
		}, status.Error(respCode, err.Error())
	}

	reqLogger.WithFields(log.Fields{
		"batch.status": pb.BatchGeocodeStatus_ACCEPTED.String(), // Accepted
	}).Info("set status on batch-cache")

	// persist the input file to long-term storage -> save to DO spaces, an S3 compatible storage system
	// update the cache with the REJECTED status if this call fails...
	go func() {

		writerCtx, cx := context.WithTimeout(context.Background(), time.Second*30)
		defer cx()

		storageLogger := log.WithFields(log.Fields{
			"batch.id": batchRequestId,
			"op":       "batchserver.storageWriter",
		})

		err := srv.PersistBatchToStorage(s.spacesClient, req, fmt.Sprintf("%s.json", batchRequestId))

		// saving to disk failed - update the status;
		if err != nil {
			storageLogger.WithFields(log.Fields{
				"err":    err,
				"status": pb.BatchGeocodeStatus_FAILED.String(),
			}).Error("failed to save batch to storage")
			_, _ = s.cacheClient.Do(context.Background(),
				"HSET", batchRequestId,
				"status", pb.BatchGeocodeStatus_FAILED.String(),
				"update_time", time.Now(),
			).Result()
			return
		}

		// saving to disk passed!
		storageLogger.WithFields(log.Fields{
			"duration": -1 * float64(startTime.Sub(time.Now()).Microseconds()) / float64(1000),
		}).Info("batch saved to storage")

		// Publish to Queue - the file is ready for workers to process
		pubsubPipe := s.pubsubClient.TxPipeline()
		pubsubPipe.Publish(writerCtx, "batch.creates", batchRequestId)
		_, err = pubsubPipe.Exec(writerCtx)
		if err != nil {
			storageLogger.WithFields(log.Fields{
				"err": err,
			}).Error("failed to publish event on batch.creates")
			_, _ = s.cacheClient.Do(writerCtx,
				"HSET", batchRequestId,
				"status", pb.BatchGeocodeStatus_FAILED.String(),
				"update_time", time.Now(),
			).Result()
			return
		}
		return
	}()

	return &pb.BatchStatusResponse{
		Id:         batchRequestId,
		Status:     pb.BatchGeocodeStatus_ACCEPTED,
		UpdateTime: timestamppb.New(time.Now()),
	}, nil

}

// StatusBatch - ...
func (s *BatchServer) GetBatchStatus(ctx context.Context, req *pb.BatchStatusRequest) (*pb.BatchStatusResponse, error) {

	var startTime = time.Now() // call on entry as proxy for use w. cobbled-together request logger
	var respCode = codes.OK    // status code; returned as part of pb.IOResponse
	var err error              // error; returned as part of pb.IOResponse

	reqLogger := log.WithFields(log.Fields{
		"method":   "/geocoder.Batch/GetBatchStatus",
		"batch.id": req.Id,
	})

	defer func() {
		if respCode == codes.OK {
			reqLogger.WithFields(log.Fields{
				"status":   respCode.String(),
				"duration": -1 * float64(startTime.Sub(time.Now()).Microseconds()) / float64(1000),
			}).Info("get batch status request ok")
		} else {
			reqLogger.WithFields(log.Fields{
				"err":      err,
				"status":   respCode.String(),
				"duration": -1 * float64(startTime.Sub(time.Now()).Microseconds()) / float64(1000),
			}).Error("get batch status request failed")
		}
	}()

	// check for status of this request from the status cache
	res, err := s.cacheClient.Do(ctx, "HMGET", req.Id, "status", "download_path", "update_time").Result()
	if err != nil {
		reqLogger.WithFields(log.Fields{
			"err": err,
		}).Error("failed to get status from batch-cache")

		respCode = codes.Unavailable // transient failure - batch status cache unavailable (?)
		return &pb.BatchStatusResponse{
			Id:     req.Id,
			Status: pb.BatchGeocodeStatus_UNDEFINED_STATUS,
		}, status.Error(respCode, err.Error())
	}

	// todo: really don't like the interface conversion here - tolerate for the time being...
	resultArr := res.([]interface{})
	batchStatus, _ := srv.SafeCast[string](resultArr[0])
	downloadPath, _ := srv.SafeCast[string](resultArr[1])

	var evtTime time.Time
	evtTime, _ = time.Parse(time.RFC3339Nano, resultArr[2].(string))

	if v, ok := pb.BatchGeocodeStatus_value[batchStatus]; ok {
		return &pb.BatchStatusResponse{
			Id:           req.Id,
			Status:       pb.BatchGeocodeStatus(v), // OK
			DownloadPath: downloadPath,
			UpdateTime:   timestamppb.New(evtTime),
		}, nil
	}

	reqLogger.Warnf("batch has unexpected state: %s", batchStatus)
	return &pb.BatchStatusResponse{
		Id:         req.Id,
		Status:     pb.BatchGeocodeStatus_UNDEFINED_STATUS,
		UpdateTime: timestamppb.New(evtTime),
	}, status.Error(codes.NotFound, "batch not found")

}

func init() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.SetLevel(log.InfoLevel)
}

func main() {
	flag.Parse()

	// init batch server object
	batchServer := &BatchServer{
		spacesClient: srv.MustSpacesClient(),
		cacheClient: srv.MustRedisClient(
			context.Background(),
			&srv.RedisClientOptions{
				DB:   *redisCacheDB,
				Host: *redisCacheHost,
				Port: *redisCachePort,
			},
		),
		pubsubClient: srv.MustRedisClient(
			context.Background(),
			&srv.RedisClientOptions{
				DB:   *pubsubDB,
				Host: *pubsubHost,
				Port: *pubsubPort,
			},
		),
	}

	// begin listening - the batch server listens for updates on `batch.status` and updates the cache
	go batchServer.Listen(context.Background(), "batch.status")

	// apply server config - `CONFIGs SET maxmemory-policy allkeys-lru`
	_, err := batchServer.cacheClient.Do(context.Background(), "CONFIG", "SET", "maxmemory-policy", "allkeys-lru").Result()
	if err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Warn("failed to set maxmemory-policy to allkeys-lru; proceed w. caution")
	}

	// register && serve
	grpcServer := grpc.NewServer([]grpc.ServerOption{}...)
	pb.RegisterBatchServer(grpcServer, batchServer)

	// listen on address and port defined from flags
	lis := srv.MustListener(serverListenAddr, serverPort)
	grpcServer.Serve(lis)
}
