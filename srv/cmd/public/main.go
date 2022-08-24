package main

import (
	// standard lib
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	// external
	redis "github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"

	// internal
	srv "github.com/dmw2151/geocoder/srv/internal"
	pb "github.com/dmw2151/geocoder/srv/proto"
)

var (
	// edge service options
	edgeServePort = flag.Int("port", 2151, "listen port for this gcaass edge service instance")
	edgeServeHost = flag.String("host", "0.0.0.0", "listen address for this gcaass edge service instance")

	// redis options
	redisCacheHost = flag.String("redis-host", "cache", "host of the redis server to use as a response cache")
	redisCachePort = flag.Int("redis-port", 6379, "host of the redis server to use as a response cache")
	redisCacheDB   = flag.Int("redis-db", 0, "db of the redis server to use as a response cache")

	// rpc service options
	rpcServerHost = flag.String("rpc-server-host", "gcaas-grpc", "host addresss of the gcaas grpc server to forward geocode requests")
	rpcServerPort = flag.Int("rpc-server-port", 50051, "port of the gcaas grpc server to forward geocode requests")
)

// edge service constants //
const (

	// edgeServiceMaxResultsCacheLimit - maximum number of results to cache; do not attempt caching requests w.
	// more than `edgeServiceMaxResultsCacheLimit`
	edgeServiceMaxResultsCacheLimit = 10

	// edgeServiceCoordinatePrecison - store location queries w. an approximate precision; this value determines
	// to what significance values are stored
	//
	// TODO: not yet properly implemented; currently just converts float to int
	edgeServiceCoordinatePrecison = 1000000

	// edgeServiceGeocodeTimeout - context deadline set on all incoming responses to /locations/; should a request to
	// `/geocoder.Geocoder/Geocode` exceed this -> raise context canceled
	edgeServiceGeocodeTimeout = 1 * time.Second

	// edgeServiceCacheDurationSeconds - TTL (in seconds!) to set on all successful responses from `/geocoder.Geocoder/Geocode`
	edgeServiceCacheDurationSeconds = 15
)

// FailedRequest - dummy struct for returning errors to client as JSON
// TODO: rename -> this isn't always a fail (e.g. `/health` endpoint)
type FailedRequest struct {
	Error string `json:"error"`
}

// GeocoderServerHandler - main handler for the edge service - attaches cache and RPC clients
type GeocoderServerHandler struct {
	rpcClient   pb.GeocoderClient
	redisClient *redis.Client
}

// Health - healthcheck - that's all...
func (gh *GeocoderServerHandler) Health(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(&FailedRequest{
		Error: "no error - up and running - everything ok",
	})
}

// Query - proxies a call to `/geocoder.Geocoder/Geocode` and returns request to client
func (gh *GeocoderServerHandler) Query(w http.ResponseWriter, r *http.Request) {

	var (
		req = &genericGeocodeRequest{} // take the incoming request; parse into struct
		res *pb.GeocodeResponse        // defined here to allow usage in deferred caching call
		err error                      // defined here to allow usage in deferred caching call
	)

	// set `edgeServiceGeocodeTimeout` on call -> VERY SHORT
	ctx, cancel := context.WithTimeout(r.Context(), edgeServiceGeocodeTimeout)
	defer cancel()

	// parse req into `genericGeocodeRequest`; throwing error and exiting if fails
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(&FailedRequest{
			Error: errors.New("invalid request body").Error(),
		})
		return
	}

	// all requests w. valid structure -> initialize a context logger for the remainder of call
	respLogger := log.WithFields(log.Fields{
		"gcaas-request-id":   ctx.Value("gcaas-request-id"),
		"request.MaxResults": req.MaxResults,
		"request.Method":     req.Method,
		"request.Query":      req.getQuery(),
	})

	// check valid - domain level checks - can we easily tell that this req will fail ?
	ok, err := req.isValid()
	if err != nil || !ok {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(&FailedRequest{
			Error: errors.Wrap(err, "invalid request body").Error(),
		})
		return
	}

	// calculate the `ReqCompositeStr` (really a long concat) and defer a call to cache a result
	var h = req.generateReqCompositeStr()
	defer func() {
		if (err == nil) && (req.MaxResults <= edgeServiceMaxResultsCacheLimit) {
			// use `ReqCompositeStr` -> `protojson.Format(res)` as key and value on the cache; protojson.Format(res)
			// much safer than trying to handle the JSON or a binary/blob representation -> let google do the work!!
			_, lerr := gh.redisClient.Do(ctx, "SET", h, protojson.Format(res), "EX", edgeServiceCacheDurationSeconds).Result()
			if lerr != nil {
				respLogger.WithFields(log.Fields{"err": lerr}).Error("caching response failed")
				return
			}
		}
	}()

	// using `ReqCompositeStr`; check the cache for the incoming request
	if req.MaxResults <= edgeServiceMaxResultsCacheLimit {
		resInterf, err := gh.redisClient.Do(ctx, "GET", h).Result()
		if err != nil {
			w.Header().Set("x-cache", "miss")
			respLogger.Debug("cache get failed; submitting to geocode server")
		} else {
			// when the cache get succeeds unmarshal the response into a new response and point to `res`
			// WARN: need to do this so the defered cache call can still use `res` :(
			cachedRes := pb.GeocodeResponse{}
			res = &cachedRes

			_ = protojson.Unmarshal([]byte(resInterf.(string)), &cachedRes)
			err := json.NewEncoder(w).Encode(cachedRes)

			if err != nil {
				respLogger.Error("parsing cache response failed")
				return
			}
			w.Header().Set("x-cache", "hit")
			respLogger.Info("cache get successful")
			return
		}
	}

	// construct request to `gh.rpcClient.Geocode` and get geocode results
	switch req.Method {
	case pb.Method_FWD_FUZZY.String():

		// oof, not the prettiest -> but a good heuristic for fuzzy match on each token
		augmentedQueryAddress := strings.Join(strings.Split(req.QueryAddress, " "), "* ") + "*"

		res, err = gh.rpcClient.Geocode(ctx, &pb.GeocodeRequest{
			Query: &pb.GeocodeRequest_AddressQuery{
				AddressQuery: augmentedQueryAddress,
			},
			MaxResults: req.MaxResults,
			Method:     pb.Method(0),
		})
	case pb.Method_REV_NEAREST.String():
		res, err = gh.rpcClient.Geocode(ctx, &pb.GeocodeRequest{
			Query: &pb.GeocodeRequest_PointQuery{
				PointQuery: &pb.Point{
					Latitude:  req.QueryLongitude,
					Longitude: req.QueryLatitude,
				},
			},
			MaxResults: req.MaxResults,
			Method:     pb.Method(1),
		})
	}

	// TODO: switch here on RPC codes -> HTTP codes; for now, treat all failures returned at
	// this stage as internal server errors; the validation should have caught common ones
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(&FailedRequest{
			Error: err.Error(),
		})
		return
	}

	// write back to the user; that's it, call it a day...
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(&FailedRequest{
			Error: errors.Wrap(err, "failed parsing /geocoder.Geocoder/Geocode response").Error(),
		})
		return
	}
}

func init() {
	log.SetFormatter(&log.JSONFormatter{TimestampFormat: "2006-01-02 15:04:05.99"})
	log.SetLevel(log.InfoLevel)
	log.SetReportCaller(false)
}

func main() {
	flag.Parse()

	// init service handler
	svcHandler := GeocoderServerHandler{
		rpcClient: srv.MustRPCClient(*rpcServerHost, *rpcServerPort),
		redisClient: srv.MustRedisClient(
			context.Background(),
			&srv.RedisClientOptions{
				DB:   *redisCacheDB,
				Host: *redisCacheHost,
				Port: *redisCachePort,
			}),
	}

	// init router
	router := mux.NewRouter().StrictSlash(true)

	// init http middlewares
	router.Use(setDefaultResponseHeadersMiddleware)
	router.Use(requestIDMiddleware)
	router.Use(loggingMiddleware)

	// init /locations/ route -> returns addresses; call to `/geocoder.Geocoder/Geocode`
	router.HandleFunc("/locations/", svcHandler.Query)
	router.HandleFunc("/health/", svcHandler.Health)

	// start server - TODO: this is not graceful at all - consider some treatment for server exit
	log.Panic(http.ListenAndServe(fmt.Sprintf("%s:%d", *edgeServeHost, *edgeServePort), router))
}
