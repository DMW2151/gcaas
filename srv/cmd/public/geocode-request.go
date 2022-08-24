package main

import (
	// standard lib
	"fmt"
	"strings"

	// internal
	srv "github.com/dmw2151/geocoder/srv/internal"
	pb "github.com/dmw2151/geocoder/srv/proto"
)

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

	if hasAddress == hasPoint {
		return false, srv.ErrMixedGeocodeArgs
	}

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
