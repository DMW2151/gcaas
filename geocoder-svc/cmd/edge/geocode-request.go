package main

import (
	// standard lib
	"fmt"
	"strings"

	// internal
	srv "github.com/dmw2151/geocoder/geocoder-svc/internal"
	pb "github.com/dmw2151/geocoder/geocoder-svc/proto"
)

type batchRequest struct {
	Method         string     `json:"method"`
	QueryAddresses []string   `json:"query_addr,omitempty"`
	QueryPoints    []pb.Point `json:"query_pts,omitempty"`
}

// isValid -
func (b *batchRequest) isValid() (bool, error) {

	hasAddresses := (len(b.QueryAddresses) > 0)
	hasPoints := (len(b.QueryPoints) > 0)

	if !(hasAddresses) && !(hasPoints) {
		return false, srv.ErrBatchMustHavePointsOrAddresses // failed on
	}

	if (hasAddresses) && (hasPoints) {
		return false, srv.ErrBatchMustHavePointsOrAddresses // failed on
	}

	if (b.Method != pb.Method_FWD_FUZZY.String()) && (b.Method != pb.Method_REV_NEAREST.String()) {
		return false, srv.ErrInvalidGeocodeMethod
	}

	if (b.Method == pb.Method_FWD_FUZZY.String()) && !(hasAddresses) {
		return false, srv.ErrInvalidForwardGeocodeRequest
	}

	if (b.Method == pb.Method_REV_NEAREST.String()) && !(hasPoints) {
		return false, srv.ErrInvalidReverseGeocodeRequest
	}

	return true, nil
}

// genericGeocodeRequest
type genericGeocodeRequest struct {
	Method         string  `json:"method"`
	MaxResults     uint32  `json:"max_results"`
	QueryAddress   string  `json:"query_addr,omitempty"`
	QueryLongitude float32 `json:"query_lng,omitempty"`
	QueryLatitude  float32 `json:"query_lat,omitempty"`
}

func (r *genericGeocodeRequest) generateReqCompositeStr() string {
	compositeStr := fmt.Sprintf(
		"%s:%d:%d:%d:%s",
		r.Method, r.MaxResults, int(r.QueryLatitude*edgeServiceCoordinatePrecison), int(r.QueryLongitude*edgeServiceCoordinatePrecison), r.QueryAddress,
	)
	return compositeStr
}

// isValid
func (r *genericGeocodeRequest) isValid() (bool, error) {

	hasAddress := (strings.Trim(r.QueryAddress, "") != "")
	hasPoint := ((r.QueryLatitude != 0.0) && (r.QueryLongitude != 0.0))

	if (r.Method != pb.Method_FWD_FUZZY.String()) && (r.Method != pb.Method_REV_NEAREST.String()) {
		return false, srv.ErrInvalidGeocodeMethod
	}

	if (r.Method == pb.Method_FWD_FUZZY.String()) && !(hasAddress) {
		return false, srv.ErrInvalidForwardGeocodeRequest
	}

	if (r.Method == pb.Method_REV_NEAREST.String()) && !(hasPoint) {
		return false, srv.ErrInvalidReverseGeocodeRequest
	}

	if (r.MaxResults < 1) || (r.MaxResults > 1024) {
		return false, srv.ErrMaxResultsOutofRange
	}

	return true, nil
}

// getQuery - utility method to...
func (r *genericGeocodeRequest) getQuery() string {
	switch r.Method {
	case pb.Method_FWD_FUZZY.String():
		return r.QueryAddress
	case pb.Method_REV_NEAREST.String():
		return fmt.Sprintf("(%.8f, %.8f)", r.QueryLatitude, r.QueryLongitude)
	}
	return ""
}
