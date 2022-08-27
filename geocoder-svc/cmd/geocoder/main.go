package main

import (
	// standard lib
	"context"
	"flag"
	"fmt"
	"math"
	"strconv"
	"strings"
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
	// grpc - gcaas server options (this service)
	serverListenAddr = flag.String("host", "0.0.0.0", "serverListenAddr (default '0.0.0.0') defines the server's listening address")
	serverPort       = flag.Int("port", 50051, "serverPort (default: 50051) defines the port to listen on")

	// redis options
	redisHost = flag.String("redis-host", "search", "host of the redis server to use as a FT engine")
	redisPort = flag.Int("redis-port", 6379, "host of the redis server to use as a FT engine")
	redisDB   = flag.Int("redis-db", 0, "db of the redis server to use as a FT engine")
)

// GeocoderServer Specific Constants //
const (

	// serverMaxQueuedTransactions - during `InsertorReplaceAddressData`, maximum number of pipelined transactions to
	// allow before calling Pipe.Exec()
	serverMaxQueuedTransactions = 1024

	// serverReverseToleranceMeters - maximum error for reverse geocoding, only results within `serverReverseToleranceMeters`
	// meters of the query point are considred
	serverReverseToleranceMeters = 1024

	// serverNFieldsForwardResponse - number of expected fields in forward response - assumes fixed across multiple methods
	serverNFieldsForwardResponse = 3

	// serverInsertionJobMaxDuration - during `InsertorReplaceAddressData`, the max duration the server will allow a
	// connection to insert data to redis
	serverInsertionJobMaxDuration = time.Second * 180
)

// GeocoderServer - server API for Geocoder service
type GeocoderServer struct {
	pb.UnimplementedGeocoderServer
	client *redis.Client
}

// handleGeocoderError
func handleGeocoderError(err error, rc *codes.Code) {
	switch err {
	case srv.ErrMalformedRedisQuery:
		c := codes.InvalidArgument
		rc = &c
	case srv.ErrRedisClient:
		c := codes.Internal
		rc = &c
	default:
		c := codes.Unknown
		rc = &c
	}
}

// handleServerResponse - handles a *very specific* format of server response from both
// forward and reverse geocoding, returns []*pb.scoredAddress
//
// WARN: makes extensive use of `srv.SafeCast[T](someInterface)` to convert the []interface{}
// we get back from the server to structs. For brevity -> no error checking here
func handleServerResponse(res interface{}, maxResults int64) ([]*pb.ScoredAddress, error) {

	resultSet, _ := srv.SafeCast[[]interface{}](res)
	nResults, _ := srv.SafeCast[int64](resultSet[0])

	// parse the result array into []*pb.ScoredAddress -> limit to smaller of n results
	// and max results to pre-allocate size...
	if maxResults < nResults {
		nResults = maxResults
	}

	var addressResults = make([]*pb.ScoredAddress, nResults)

	if cap(addressResults) == 0 {
		return addressResults, status.Error(codes.OK, "ok")
	}

	// Grab the `maxConfidence` from the first result -> produce a normalized confidence for each
	// result in the set...
	//
	// NOTE: assumes results sorted in order of best -> worst match
	confidenceStr, _ := srv.SafeCast[string](resultSet[2])
	maxConfidence, _ := strconv.ParseFloat(confidenceStr, 32)

	for i := int64(0); i < nResults; i++ {

		resultStartPosition := (i * serverNFieldsForwardResponse) + 1

		// extract ID
		Id, _ := srv.SafeCast[string](resultSet[resultStartPosition])

		// extract confidence score
		confScoreStr, _ := srv.SafeCast[string](resultSet[resultStartPosition+1])
		confScore, _ := strconv.ParseFloat(confScoreStr, 32)

		// extract details (e.g. lat, lng, address...)
		addressDetails, _ := srv.SafeCast[[]interface{}](resultSet[resultStartPosition+2])

		locationStr, _ := srv.SafeCast[string](addressDetails[1])
		pt := srv.PointFromLocationString(locationStr)

		fullStreetAddress, _ := srv.SafeCast[string](addressDetails[3])

		// append all results to addressresults...
		addressResults[i] = &pb.ScoredAddress{
			Address: &pb.Address{
				Location: pt,
				Id:       Id,
			},
			NormedConfidence:  float32(confScore / maxConfidence),
			FullStreetAddress: fullStreetAddress,
		}
	}
	return addressResults, nil
}

// Forward - run forward geocoding via call to `/geocoder.Geocoder/Geocode`
func (s *GeocoderServer) Forward(ctx context.Context, req *pb.GeocodeRequest) ([]*pb.ScoredAddress, error) {

	addrQuery := req.GetAddressQuery()

	// check conditions we KNON the db server would fail (e.g. addrQuery is `null`) or `req.MaxResults < 0`
	if strings.Trim(addrQuery, "") == "" {
		return nil, srv.ErrMalformedRedisQuery
	}

	res, err := s.client.Do(
		ctx, "FT.SEARCH", "addr-idx", fmt.Sprintf("@composite_street_address:%s", addrQuery),
		"WITHSCORES", "LANGUAGE", "english", "SCORER", "TFIDF.DOCNORM", "LIMIT", "0", req.MaxResults,
	).Result()

	// some unknown error preventing results -> raise as internal :(
	if err != nil {
		return nil, srv.ErrRedisClient
	}

	return handleServerResponse(res, int64(req.MaxResults))
}

// Reverse - run reverse geocoding via call to `/geocoder.Geocoder/Geocode`
func (s *GeocoderServer) Reverse(ctx context.Context, req *pb.GeocodeRequest) ([]*pb.ScoredAddress, error) {

	ptQuery := req.GetPointQuery()

	// check conditions we KNON the db server would fail (e.g. addrQuery is `null`) or `req.MaxResults < 0`
	// here, check query contains invalid coordinates -> throw malformed reqquest
	if math.Abs(float64(ptQuery.Latitude)) > 90 || math.Abs(float64(ptQuery.Longitude)) > 180 {
		return nil, srv.ErrMalformedRedisQuery
	}

	res, err := s.client.Do(
		ctx, "FT.SEARCH", "addr-idx",
		fmt.Sprintf("@location:[%.8f %.8f %d m]", ptQuery.Longitude, ptQuery.Latitude, serverReverseToleranceMeters),
		"WITHSCORES", "LIMIT", "0", req.MaxResults,
	).Result()

	// some uncaught error preventing results -> raise as internal :(
	if err != nil {
		return nil, srv.ErrRedisClient
	}

	// parse response && return to client
	return handleServerResponse(res, int64(req.MaxResults))
}

// Geocode - call the GRPC server `/geocoder.Geocoder/Geocode` method
func (s *GeocoderServer) Geocode(ctx context.Context, req *pb.GeocodeRequest) (*pb.GeocodeResponse, error) {

	var startTime = time.Now()             // call on entry as proxy for use w. cobbled-together request logger
	var respCode = codes.OK                // status code; returned as part of pb.IOResponse
	var err error                          // error; returned as part of pb.IOResponse
	var addressResults []*pb.ScoredAddress // list of result addresses; returned as part of pb.IOResponse

	defer func() {
		reqLogger := log.WithFields(log.Fields{
			"request.max_results": req.MaxResults,
			"request.method":      req.Method,
			"request.Query":       req.GetQuery(),
			"duration":            -1 * float64(startTime.Sub(time.Now()).Microseconds()) / float64(1000),
			"method":              "/geocoder.Geocoder/Geocode",
			"status":              respCode.String(),
		})

		if respCode == codes.OK {
			reqLogger.Info("geocode request successful")
			return
		}

		reqLogger.WithFields(log.Fields{
			"err": err,
		}).Error("geocode request failed")
	}()

	switch req.Method {
	case pb.Method_FWD_FUZZY:
		addressResults, err = s.Forward(ctx, req)
		if err != nil {
			handleGeocoderError(err, &respCode)
			return nil, status.Errorf(respCode, err.Error())
		}

	case pb.Method_REV_NEAREST:
		addressResults, err = s.Reverse(ctx, req)
		if err != nil {
			handleGeocoderError(err, &respCode)
			return nil, status.Errorf(respCode, err.Error())
		}
	}

	return &pb.GeocodeResponse{
		Result:     addressResults,
		NumResults: uint32(len(addressResults)),
	}, nil
}

// indexesReady - creates || checks the existence an index required for the application
// Indexes the 'location' and `composite_street_address` fields of all hashes starting w. `address:*`
// and elects for LOW MEMORY options where possible.
func (s *GeocoderServer) indexesReady() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.client.Do(
		ctx, "FT.CREATE", "addr-idx", "ON", "HASH", "PREFIX", "1", "address:", "NOHL", "NOOFFSETS",
		"LANGUAGE", "english", "SCHEMA", "location", "GEO", "composite_street_address", "TEXT", "SORTABLE",
	).Result()

	if err != nil {
		// expected error -> will throw on all server starts after the first unless db wiped
		if err.Error() == "Index already exists" {
			log.WithFields(log.Fields{
				"err": err.Error(),
			}).Warn("failed to create FT index on server init")
			return nil
		}

		// on uncrecoverable error - e.g. failed connection to DB; throw error
		log.WithFields(log.Fields{
			"err": err.Error(),
		}).Error("failed to create FT index on server init")
		return status.Errorf(codes.FailedPrecondition, err.Error())
	}
	return nil
}

func init() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.SetLevel(log.InfoLevel)
}

func main() {
	flag.Parse()

	// init geocoder server object
	geocoderServer := &GeocoderServer{
		client: srv.MustRedisClient(
			context.Background(),
			&srv.RedisClientOptions{
				DB:   *redisDB,
				Host: *redisHost,
				Port: *redisPort,
			},
		),
	}

	err := geocoderServer.indexesReady()
	if err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Panic("failed to create default indexes")
	}

	// register && serve
	grpcServer := grpc.NewServer([]grpc.ServerOption{}...)
	pb.RegisterGeocoderServer(grpcServer, geocoderServer)

	// listen on address and port from flags
	lis := srv.MustListener(serverListenAddr, serverPort)
	grpcServer.Serve(lis)
}
