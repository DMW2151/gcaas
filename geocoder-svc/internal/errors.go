package srv

import (
	"errors"
)

var (

	// ErrFailedTypeConversion - catchall error for failures related to type conversion of redis' []interface{}
	// responses -> prefer throwing `ErrFailedTypeConversion` and returning nil to panic & exit...
	ErrFailedTypeConversion = errors.New("failed interface type conversion")

	// ErrMalformedRedisQuery is returned when the request body will yield a bad response from the db server
	// should be thrown during validation, before sending to db server
	ErrMalformedRedisQuery = errors.New("malformed geocode request")

	// ErrRedisClient is returned when the request body yielded a bad response - NOT caught by validation
	// prefer `ErrMalformedRedisQuery` to `ErrRedisClient`
	ErrRedisClient = errors.New("redis client error")

	// ErrInvalidForwardGeocodeRequest -
	ErrInvalidForwardGeocodeRequest = errors.New("forward geocode requests must have a valid `query_addr`")

	// ErrInvalidReverseGeocodeRequest -
	ErrInvalidReverseGeocodeRequest = errors.New("reverse geocode requests must have a valid `query_lat` and `query_lng`")

	// ErrInvalidGeocodeMethod -
	ErrInvalidGeocodeMethod = errors.New("`method` must be one of (`FWD_FUZZY`, `REV_NEAREST`)")

	// ErrMaxResultsOutofRange -
	ErrMaxResultsOutofRange = errors.New("`max_results` must be an int between 1 and 1024")

	// ErrBatchMustHavePointsOrAddresses - 
	ErrBatchMustHavePointsOrAddresses = errors.New("batches must have points *or* addresses")

	// ErrEnvironmentNotSet - 
	ErrEnvironmentNotSet = errors.New("expected environment var not set")
)
