package srv

import (

	// standard lib
	"fmt"

	// external
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	// internal
	pb "github.com/dmw2151/geocoder/srv/proto"
)

// MustRPCClient - creates a new RPC client or panics
func MustRPCClient(serverHost string, serverPort int) pb.GeocoderClient {
	var dialOptions = []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.Dial(fmt.Sprintf("%s:%d", serverHost, serverPort), dialOptions...)
	if err != nil {
		log.WithFields(log.Fields{
			"err":  err,
			"host": serverHost,
			"port": serverPort,
		}).Panic("failed to dial GRPC server")
	}

	// return API client
	return pb.NewGeocoderClient(conn)
}
