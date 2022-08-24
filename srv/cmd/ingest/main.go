package main

import (

	// standard lib
	"bufio"
	"bytes"
	"context"
	"flag"
	"github.com/pkg/errors"
	"io"
	"os"
	"strconv"
	"strings"

	// internal
	srv "github.com/dmw2151/geocoder/srv/internal"
	pb "github.com/dmw2151/geocoder/srv/proto"

	// external
	log "github.com/sirupsen/logrus"
)

var (
	// rpc service options
	rpcServerHost = flag.String("rpc-server", "gcaas-grpc", "host addresss of the gcaas grpc server to forward ingest requests")
	rpcServerPort = flag.Int("rpc-server-port", 50051, "port of the gcaas grpc server to forward ingesst requests")

	// file processing options
	targetFile = flag.String("file", "./../../../_data/nyc/Address_Point.csv", "The file to load for geocoder demo")

	// redis options
	redisHost = flag.String("redis-host", "redis", "host of the redis server to use as a FT engine")
	redisPort = flag.Int("redis-port", 6379, "host of the redis server to use as a FT engine")
	redisDB   = flag.Int("redis-db", 0, "db of the redis server to use as a FT engine")
)

// nColsDOBReport ...
const nColsDOBReport = 23

// slimLineCounter ...
// https://stackoverflow.com/questions/24562942/golang-how-do-i-determine-the-number-of-lines-in-a-file-efficiently
func slimLineCounter(r io.Reader) (numTotalLines int, err error) {

	buf := make([]byte, bufio.MaxScanTokenSize)

	for {
		// read through contents of io.Reader
		var buffPosition int

		bufferSize, err := r.Read(buf)
		if err != nil && err != io.EOF {
			log.WithFields(log.Fields{
				"err": err,
			}).Error("failed reading from buffer")
			return 0, err
		}

		// read to next newline char - adding to the count of lines and current position
		for {
			i := bytes.IndexByte(buf[buffPosition:], '\n')
			if i == -1 || bufferSize == buffPosition {
				break
			}
			buffPosition += i + 1
			numTotalLines++
		}

		if err == io.EOF {
			return numTotalLines, nil
		}
	}
}

// csvToAddressProto
func csvToAddressProto(fp string) ([]*pb.Address, error) {

	// open file && pcount the number of addresses to allocate []*pb.Address
	// with fixed length
	fi, err := os.Open(fp)
	if err != nil {
		return nil, errors.Wrap(err, "failed reading data file")
	}

	nAddresses, err := slimLineCounter(fi)
	if err != nil {
		return nil, errors.Wrap(err, "failed counting")
	}

	// init pre-allocated array for addresses - assumes a small-ish number
	// of addresses (10**7)
	var (
		addresses = make([]*pb.Address, nAddresses-1)
		addrIdx   = 0
	)

	fi.Seek(0, 0)
	scanner := bufio.NewScanner(fi)

	for scanner.Scan() {
		if addrIdx > 0 {
			// Extract Location and Address Data From CSV
			content := strings.Split(scanner.Text(), ",")
			if len(content) != nColsDOBReport {
				log.WithFields(log.Fields{
					"n_expected_elems": nColsDOBReport,
					"n_observed_elems": len(content),
				}).Warn("unexpected data struct; skipping")
			}

			// Location uses a string representaton of the point -> e.g. `POINT (-73.94890840262882 40.681024605257534)`
			addrLocation := srv.PointFromLocationString(content[0])
			boroInt, _ := strconv.Atoi(content[8])

			// Compose result and apppend to addresses to send
			addresses[addrIdx-1] = &pb.Address{
				Id:       content[2] + content[1], // concatenation of the building ID and the address ID
				Location: addrLocation,            // extracted from pointFromString
				Data: &pb.AddressData{
					HouseNum:       content[3],       // e.g. "451A",
					FullStreetName: content[22],      // e.g. "WINTHROP ST "
					Borocode:       pb.Boro(boroInt), // e.g. "3" -> "BROOKLYN"
					Zipcode:        content[9],       // e.g  "11203-XXXX"
				},
			}
		}
		addrIdx++
	}

	return addresses, nil
}

// writeNYCAddresses sends a sequence of points to server and expects to get a RouteSummary from server.
func writeNYCAddresses(client pb.GeocoderClient, path string) {

	// defer cancellation until done
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// call rpc - open connection and begin streaming address points into the service
	stream, err := client.InsertorReplaceAddressData(ctx)
	if err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Error("/geocoder.Geocoder/InsertorReplaceAddressData; failed initializing stream")
	}

	// iterate thru the parsed csv data, queuing up to the server
	addresses, err := csvToAddressProto(path)
	if err != nil {
		log.WithFields(log.Fields{
			"err":      err,
			"filepath": path,
		}).Error("failed processing source file")
	}

	for _, a := range addresses {
		if err := stream.Send(a); err != nil {
			log.WithFields(log.Fields{
				"err": err,
				"msg": a,
			}).Error("/geocoder.Geocoder/InsertorReplaceAddressData; failed stream.Send()")
		}
	}

	// get response back from server
	reply, err := stream.CloseAndRecv()
	if err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Error("/geocoder.Geocoder/InsertorReplaceAddressData; failed recv")
	}

	log.WithFields(log.Fields{
		"insert.success":     reply.Success,
		"insert.num_objects": reply.TotalObjectsWritten,
	}).Info("/geocoder.Geocoder/InsertorReplaceAddressData; success")

}

func main() {
	flag.Parse()

	// Init client and insert test data
	client := srv.MustRPCClient(*serverHost, *serverPort)
	writeNYCAddresses(client, *targetFile)

}
