package main

import (

	// standard lib
	"context"
	"flag"
	"fmt"

	// internal
	srv "github.com/dmw2151/geocoder/geocoder-svc/internal"
	pb "github.com/dmw2151/geocoder/geocoder-svc/proto"

	// external
	"github.com/aws/aws-sdk-go/service/s3"
	redis "github.com/go-redis/redis/v8"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

var (
	// queue parameters
	pubsubHost = flag.String("pubsub-host", "pubsub", "...")
	pubsubPort = flag.Int("pubsub-port", 6379, "...")
	pubsubDB   = flag.Int("pubsub-db", 0, "...")
)

// GeocoderServer - server API for Geocoder service
type Worker struct {
	pubsubClient *redis.Client
	spacesClient *s3.S3
	listenTopic  string
	replyTopic   string
}

func (w *Worker) updateBatchJobStatus(ctx context.Context, id string, bs pb.BatchGeocodeStatus, dlfp string) {

	updatedJobStatus := pb.BatchStatusResponse{
		Id:           id,
		Status:       bs,
		DownloadPath: dlfp,
	}

	b, _ := proto.Marshal(&updatedJobStatus)

	pubsubPipe := w.pubsubClient.TxPipeline()
	pubsubPipe.Publish(ctx, w.replyTopic, b)
	_, err := pubsubPipe.Exec(ctx)
	if err != nil {
		log.WithFields(log.Fields{
			"err":          err,
			"batch.id":     id,
			"batch.status": bs.String(),
		}).Error("failed to publish status event on batch.status")
	}
}

// Listen - the batch server listens with one client (pubsub) and writes to cache
// with the other
func (w *Worker) Listen(ctx context.Context) {
	sub := w.pubsubClient.Subscribe(ctx, w.listenTopic)
	defer func() {
		log.Infof("exit from pub/sub channel: %s", w.listenTopic)
		sub.Unsubscribe(w.pubsubClient.Context(), w.listenTopic)
	}()

	channel := sub.Channel()

	// marshall msg from the pub/sub channel and write to cache
	for msg := range channel {
		log.Infof("worker recv msg on %s", w.listenTopic)

		go func() {
			var cbr pb.CreateBatchRequest
			var batchResults pb.BatchResponse

			// get the file from spaces
			Id := msg.Payload
			baseFileKey := fmt.Sprintf("%s.json", Id)
			resultsFileKey := fmt.Sprintf("%s-results.json", Id)

			// tell other services this job has been picked by a worker
			w.updateBatchJobStatus(ctx, Id, pb.BatchGeocodeStatus_IN_QUEUE, "")

			// get the batch from DO spaces...
			err := srv.GetBatchFromStorage(w.spacesClient, baseFileKey, &cbr)
			if err != nil {
				log.WithFields(log.Fields{
					"err":          err,
					"batch.id":     Id,
					"batch.status": pb.BatchGeocodeStatus_FAILED.String(),
				}).Error("batch failed in download from spaces")
				w.updateBatchJobStatus(ctx, Id, pb.BatchGeocodeStatus_FAILED, "")
				return
			}

			// process the file
			if err != nil {
				log.WithFields(log.Fields{
					"err":          err,
					"batch.id":     Id,
					"batch.status": pb.BatchGeocodeStatus_FAILED.String(),
				}).Error("batch failed in geocoding")
				w.updateBatchJobStatus(ctx, Id, pb.BatchGeocodeStatus_FAILED, "")
				return
			}

			// upload result to DO spaces
			err = srv.PersistBatchToStorage(w.spacesClient, batchResults, resultsFileKey)
			if err != nil {
				log.WithFields(log.Fields{
					"err":          err,
					"batch.id":     Id,
					"batch.status": pb.BatchGeocodeStatus_FAILED.String(),
				}).Error("batch failed in saving results to spaces")
				w.updateBatchJobStatus(ctx, Id, pb.BatchGeocodeStatus_FAILED, "")
				return
			}

			// create a pre-signed URL for the file
			downloadPath, err := srv.GeneratePresignedURL(w.spacesClient, resultsFileKey)
			if err != nil {
				log.WithFields(log.Fields{
					"err":          err,
					"batch.id":     Id,
					"batch.status": pb.BatchGeocodeStatus_FAILED.String(),
				}).Error("batch failed in generating download URL")
				w.updateBatchJobStatus(ctx, Id, pb.BatchGeocodeStatus_FAILED, "")
				return
			}

			// success
			w.updateBatchJobStatus(ctx, Id, pb.BatchGeocodeStatus_SUCCESS, downloadPath)
		}()

	}

}

func init() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.SetLevel(log.InfoLevel)
}

func main() {
	flag.Parse()

	// init batch server object
	worker := &Worker{
		spacesClient: srv.MustSpacesClient(),
		listenTopic:  "batch.creates",
		replyTopic:   "batch.status",
		pubsubClient: srv.MustRedisClient(
			context.Background(),
			&srv.RedisClientOptions{
				DB:   *pubsubDB,
				Host: *pubsubHost,
				Port: *pubsubPort,
			},
		),
	}

	// begin listening - the worker server listens for updates on `batch.creates` and replies on `batch.status`
	worker.Listen(context.Background())
}
