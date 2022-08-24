# Geocoding with Redis


// Geocoder GRPC Service //

// Starting The GeocoderServer //
// ./main --host localhost --port 50051

// Sample Calls to GeocoderServer using the grpc_cli on localhost //
// See also: https://github.com/grpc/grpc/blob/master/doc/command_line_tool.md

// Forward :: `54 MACON ST BROOKLYN 11216` -> `-73.9422 40.6825` //
// Forward :: FT.SEARCH addr-idx '@composite_street_address:54 MACON ST BROOKLYN 11216' WITHSCORES LANGUAGE english LIMIT 0 1
// grpc_cli call localhost:50051 Geocode 'address_query: "MACON", max_results: 3, method: 0' --protofiles=proto/geocoder.proto

// Reverse ::  `-73.9422 40.6825` -> `54 MACON ST BROOKLYN 11216` //
// Reverse :: FT.SEARCH addr-idx '@location:[-73.9422 40.6825 5 km]' LIMIT 0 1
// grpc_cli call localhost:50051 Geocode 'point_query: {latitude: -73.9422, longitude: 40.6825}, max_results: 3, method: 1' --protofiles=proto/geocoder.proto


`grpc_cli call _gcaas._tcp.dmw2151.com Geocode 'point_query: {latitude: -73.9422, longitude: 40.6825}, max_results: 3, method: 1' --protofiles=srv/proto/geocoder.proto`





// HTTP API //

// curl -XPOST localhost:2151/locations/ -d '{"method": "FWD_FUZZY", "max_results": 3, "query_addr": "MACON"}'
// curl -XPOST localhost:2151/locations/ -d '{"method": "REV_NEAREST", "max_results": 1, "query_lat": -73.9489, "query_lng": 40.6}'



// Building

from `srv` 
	-> `docker build -t dmw2151/gcaas --file ./cmd/grpc/Dockerfile .`
	-> `docker build -t dmw2151/gcaas-edge --file ./cmd/public/Dockerfile . `
