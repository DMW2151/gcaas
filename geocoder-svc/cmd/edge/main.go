package main

import (
	// standard lib
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"regexp"
	"time"

	// external
	redis "github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"

	// internal
	srv "github.com/dmw2151/geocoder/geocoder-svc/internal"
	pb "github.com/dmw2151/geocoder/geocoder-svc/proto"
)

var (
	// edge service options
	edgeServeHost = flag.String("host", "0.0.0.0", "listen address for this gcaass edge service instance")
	edgeServePort = flag.Int("port", 2151, "listen port for this gcaass edge service instance")

	// redis options
	redisCacheHost = flag.String("redis-host", "edge-cache", "host of the redis server to use as a response cache")
	redisCachePort = flag.Int("redis-port", 6379, "host of the redis server to use as a response cache")
	redisCacheDB   = flag.Int("redis-db", 0, "db of the redis server to use as a response cache")

	// rpc service options
	geocoderServerHost = flag.String("rpc-server-host", "gcaas-geocoder", "host addresss of the gcaas grpc server to forward geocode requests")
	geocoderServerPort = flag.Int("rpc-server-port", 50051, "port of the gcaas grpc server to forward geocode requests")

	// rpc service options
	batchServerHost = flag.String("batch-server-host", "gcaas-batch", "host addresss of the gcaas batch server to forward batch requests")
	batchServerPort = flag.Int("batch-server-port", 50053, "port of the gcaas batch server to forward batch requests")
)

const (
	// edgeServiceMaxResultsCacheLimit - maximum number of requested results to cache;
	edgeServiceMaxResultsCacheLimit = 10

	// edgeServiceCoordinatePrecison - store location queries w. an approximate precision;
	edgeServiceCoordinatePrecison = 1000000

	// edgeServiceRequestTimeout - context deadline set on all responses to /geocode/;
	edgeServiceRequestTimeout = 1 * time.Second

	// edgeServiceCacheDurationSeconds - TTL (in seconds!) to set on all successful responses from `/geocoder.Geocoder/Geocode`
	edgeServiceCacheDurationSeconds = 90
)

var requestRegexp = regexp.MustCompile(`(\w{3,})`)

// EdgeErrorResponse - dummy struct for returning errors to client as JSON
// TODO: rename -> this isn't always a fail (e.g. `/health` endpoint)
type EdgeErrorResponse struct {
	Error string `json:"error"`
}

// GeocoderServerHandler - main handler for the edge service - attaches cache and RPC clients
type GeocoderServerHandler struct {
	geocoderClient pb.GeocoderClient
	batchClient    pb.BatchClient
	redisClient    *redis.Client
}

// Health - healthcheck - that's all...
func (gh *GeocoderServerHandler) Health(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(&EdgeErrorResponse{
		Error: "no error - up and running - everything ok",
	})
}

// BatchCreate ....
func (gh *GeocoderServerHandler) CreateBatch(w http.ResponseWriter, r *http.Request) {

	var req = &batchRequest{}

	// set `edgeServiceGeocodeTimeout` on call -> LONG
	ctx, cancel := context.WithTimeout(r.Context(), edgeServiceRequestTimeout)
	defer cancel()

	// parse req into `genericGeocodeRequest`; throwing error and exiting if fails
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(&EdgeErrorResponse{
			Error: errors.New("invalid request body").Error(),
		})
		return
	}

	respLogger := log.WithFields(log.Fields{
		"gcaas-request-id": ctx.Value("gcaas-request-id"),
		"request.Method":   req.Method,
	})

	// check valid - domain level checks - can we easily tell that this req will fail ?
	ok, err := req.isValid()
	if err != nil || !ok {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(&EdgeErrorResponse{
			Error: errors.Wrap(err, "invalid request body").Error(),
		})
		return
	}

	// put points into an array
	pts := make([]*pb.Point, len(req.QueryPoints))
	for i, p := range req.QueryPoints {
		pts[i] = &p
	}

	// Method_value str -> int => this is very defensive; should be handled in
	// isValid() call
	method, ok := pb.Method_value[req.Method]
	if !ok {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(&EdgeErrorResponse{
			Error: errors.Wrap(err, "invalid request body").Error(),
		})
	}

	batchCreateResponse, err := gh.batchClient.CreateBatch(ctx, &pb.CreateBatchRequest{
		Method:    pb.Method(method),
		Addresses: req.QueryAddresses,
		Points:    pts,
	})

	// on falure ...
	if err != nil {
		respLogger.Error("/geocoder.Batch/CreateBatch call failed")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(&EdgeErrorResponse{
			Error: err.Error(),
		})
		return
	}

	// on success -> write back to the user; that's it, call it a day...
	err = json.NewEncoder(w).Encode(batchCreateResponse)
	if err != nil {
		respLogger.Error("failed parsing /geocoder.Batch/CreateBatch response")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(&EdgeErrorResponse{
			Error: errors.Wrap(err, "failed parsing /geocoder.Batch/CreateBatch response").Error(),
		})
		return
	}
}

// BatchGetStatus ....
func (gh *GeocoderServerHandler) BatchGetStatus(w http.ResponseWriter, r *http.Request) {

	ctx, cancel := context.WithTimeout(r.Context(), edgeServiceRequestTimeout)
	defer cancel()

	// parse vars...
	vars := mux.Vars(r)
	id, _ := vars["id"]

	respLogger := log.WithFields(log.Fields{
		"gcaas-request-id": ctx.Value("gcaas-request-id"),
		"request.BatchId":  id,
	})

	if _, err := uuid.Parse(id); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		respLogger.Error("batch uuid (`id`) not a valid uuid")
		json.NewEncoder(w).Encode(&EdgeErrorResponse{
			Error: errors.New("invalid request url; expect GET request to `/batch/${batch-uuid}`").Error(),
		})
		return
	}

	batchStatusResponse, err := gh.batchClient.GetBatchStatus(ctx, &pb.BatchStatusRequest{
		Id: id,
	})

	// on falure ...
	if err != nil {
		if err == redis.Nil {
			respLogger.Warn("/geocoder.Batch/BatchStatus call successful; no result")
			w.WriteHeader(http.StatusNotFound)
		} else {
			respLogger.Warn("/geocoder.Batch/BatchStatus call failed")
			w.WriteHeader(http.StatusInternalServerError)
		}
		json.NewEncoder(w).Encode(&EdgeErrorResponse{
			Error: err.Error(),
		})
		return
	}

	// on success -> write back to the user; that's it, call it a day...
	err = json.NewEncoder(w).Encode(batchStatusResponse)
	if err != nil {
		respLogger.Error("failed parsing /geocoder.Batch/BatchStatus response")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(&EdgeErrorResponse{
			Error: errors.Wrap(err, "failed parsing /geocoder.Batch/BatchStatus response").Error(),
		})
		return
	}
}

// Query - proxies a call to `/geocoder.Geocoder/Geocode` and returns request to client
func (gh *GeocoderServerHandler) Query(w http.ResponseWriter, r *http.Request) {

	var req = &genericGeocodeRequest{} // take the incoming request; parse into struct
	var res *pb.GeocodeResponse        // defined here to allow usage in deferred caching call
	var err error                      // defined here to allow usage in deferred caching call

	ctx, cancel := context.WithTimeout(r.Context(), edgeServiceRequestTimeout)
	defer cancel()

	// parse req into `genericGeocodeRequest`; throwing error and exiting if fails
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(&EdgeErrorResponse{
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
		json.NewEncoder(w).Encode(&EdgeErrorResponse{
			Error: errors.Wrap(err, "invalid request body").Error(),
		})
		return
	}

	// CACHE SET w. `generateReqCompositeStr` (really a long concat) and defer a call to cache a result
	var h = req.generateReqCompositeStr()
	defer func() {
		if (err == nil) && (req.MaxResults <= edgeServiceMaxResultsCacheLimit) {
			// use `generateReqCompositeStr` -> `protojson.Format(res)` as key and value on the cache; protojson.Format(res)
			// much safer than trying to handle the JSON or a binary/blob representation -> let google do the work!!
			_, lerr := gh.redisClient.Do(ctx, "SET", h, protojson.Format(res), "EX", edgeServiceCacheDurationSeconds).Result()
			if lerr != nil {
				respLogger.WithFields(log.Fields{"err": lerr}).Error("caching response failed")
				return
			}
		}
	}()

	// CACHE GET
	// WARN: when the cache get succeeds unmarshal the response into a new response, `cachedRes`
	// and point to `res`, need to do this so the defered cache call can still use `res` :(
	if req.MaxResults <= edgeServiceMaxResultsCacheLimit {
		resInterf, err := gh.redisClient.Do(ctx, "GET", h).Result()
		if err != nil {
			w.Header().Set("x-cache", "miss")
			respLogger.Debug("cache get failed; submitting to geocode server")
		} else {
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

	// construct request to `gh.geocoderClient.Geocode` and get geocode results
	switch req.Method {
	case pb.Method_FWD_FUZZY.String():
		res, err = gh.geocoderClient.Geocode(ctx, &pb.GeocodeRequest{
			Query: &pb.Query{
				Query: &pb.Query_AddressQuery{
					AddressQuery: requestRegexp.ReplaceAllString(req.QueryAddress, "%$1%"), // Levenstien distance of 1 on all words...
				},
			},
			MaxResults: req.MaxResults,
			Method:     pb.Method_FWD_FUZZY,
		})
	case pb.Method_REV_NEAREST.String():
		res, err = gh.geocoderClient.Geocode(ctx, &pb.GeocodeRequest{
			Query: &pb.Query{
				Query: &pb.Query_PointQuery{
					PointQuery: &pb.Point{
						Latitude:  req.QueryLatitude,
						Longitude: req.QueryLongitude,
					},
				},
			},
			MaxResults: req.MaxResults,
			Method:     pb.Method_REV_NEAREST,
		})
	}

	// TODO: should be a switch on RPC codes -> HTTP codes; for now, currently all internal errors...
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(&EdgeErrorResponse{
			Error: err.Error(),
		})
		return
	}

	// write server respoonse back out to the caller...
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(&EdgeErrorResponse{
			Error: errors.Wrap(err, "failed parsing /geocoder.Geocoder/Geocode response").Error(),
		})
		return
	}
}

func init() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.SetLevel(log.InfoLevel)
}

func main() {
	flag.Parse()

	geocoderConn := srv.MustRPCClient(*geocoderServerHost, *geocoderServerPort)
	batchConn := srv.MustRPCClient(*batchServerHost, *batchServerPort)

	// init edge service object
	svcHandler := GeocoderServerHandler{
		geocoderClient: pb.NewGeocoderClient(geocoderConn),
		batchClient:    pb.NewBatchClient(batchConn),
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
	router.HandleFunc("/geocode/", svcHandler.Query).Methods("POST")
	router.HandleFunc("/batch/", svcHandler.CreateBatch).Methods("POST")
	router.HandleFunc("/batch/{id}", svcHandler.BatchGetStatus).Methods("GET")
	router.HandleFunc("/health/", svcHandler.Health).Methods("GET")

	// start server - TODO: this is not graceful at all - consider some treatment for server exit
	log.Panic(http.ListenAndServe(fmt.Sprintf("%s:%d", *edgeServeHost, *edgeServePort), router))
}
