package srv

import (
	// standard lib
	"regexp"
	"strconv"

	// internal
	pb "github.com/dmw2151/geocoder/geocoder-svc/proto"
)

// isNumeric - regex pattern for extracting digits
var isNumeric = regexp.MustCompile(`[-]?\d[\d,]*[\.]?[\d{2}]*`)

// PointFromLocationString - utility function for handling redis SEARCH's representation
// of coordinates as a single string
func PointFromLocationString(s string) *pb.Point {

	submatchall := isNumeric.FindAllString(s, -1)
	if len(submatchall) != 2 {
		return &pb.Point{}
	}

	// as a rule, latitude preceedes longitude, THE N/S coordinate comes before the E/W coord
	// NYC flipped these... :(
	lat, xerr := strconv.ParseFloat(submatchall[1], 32)
	lng, yerr := strconv.ParseFloat(submatchall[0], 32)

	if (xerr != nil) || (yerr != nil) {
		return &pb.Point{}
	}

	return &pb.Point{
		Latitude:  float32(lat),
		Longitude: float32(lng),
	}
}

// SafeCast - utility function for handling interface type conversions (more) safely
// useful for handling redis custom commands returning []interface{}...
func SafeCast[T any](i interface{}) (T, error) {
	var empty T
	if r, ok := i.(T); ok {
		return r, nil
	}
	return empty, ErrFailedTypeConversion
}
