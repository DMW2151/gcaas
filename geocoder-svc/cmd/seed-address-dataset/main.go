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
	"strings"
	"time"

	// internal
	srv "github.com/dmw2151/geocoder/geocoder-svc/internal"
	pb "github.com/dmw2151/geocoder/geocoder-svc/proto"

	// external
	log "github.com/sirupsen/logrus"
)

var (
	// rpc service options
	rpcServerHost = flag.String("rpc-server", "gc-grpc.dmw2151.com", "host addresss of the gcaas grpc server to forward ingest requests")
	rpcServerPort = flag.Int("rpc-server-port", 50052, "port of the gcaas grpc server to forward ingesst requests")

	// file processing options
	targetFile = flag.String("file", "./../misc/data-processing/_data/prepared_nyc.csv", "The file to load for geocoder demo")
)

// nColsDOBReport ...
const expectedColumnsInputData = 3

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

// fileToAddressProtoArray -
func fileToAddressProtoArray(fp string) ([]*pb.Address, error) {

	// open file && pcount the number of addresses to allocate []*pb.Address with fixed length
	fi, err := os.Open(fp)
	if err != nil {
		return nil, errors.Wrap(err, "failed reading data file")
	}

	nAddresses, err := slimLineCounter(fi)
	if err != nil {
		return nil, errors.Wrap(err, "failed counting")
	}

	// init pre-allocated array for addresses - assumes a small-ish number of addresses (10**7)
	var addresses = make([]*pb.Address, nAddresses-1)
	var addrIdx = 0

	fi.Seek(0, 0)
	scanner := bufio.NewScanner(fi)

	for scanner.Scan() {
		if addrIdx > 0 {
			// Extract Location and Address Data From CSV
			content := strings.Split(scanner.Text(), ",")

			if len(content) != expectedColumnsInputData {
				log.WithFields(log.Fields{
					"n_expected_elems": expectedColumnsInputData,
					"n_observed_elems": len(content),
				}).Warn("unexpected data struct; skipping")
			}

			// NOTE: DANGEROUS FLAG...
			addrLocation := srv.PointFromLocationString(content[1], true)

			// Compose result and apppend to addresses to send
			addresses[addrIdx-1] = &pb.Address{
				Id:                     content[0],
				Location:               addrLocation,
				CompositeStreetAddress: content[2],
			}
		}
		addrIdx++
	}

	return addresses, nil
}

// writeAddressData sends a sequence of points to server and expects to get a RouteSummary from server.
func writeAddressData(client pb.ManagementClient, path string) {

	// defer cancellation until done
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*180)
	defer cancel()

	// call rpc - open connection and begin streaming address points into the service
	stream, err := client.InsertorReplaceAddressData(ctx)
	if err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Error("/geocoder.Management/InsertorReplaceAddressData; failed initializing stream")
	}

	// iterate thru the parsed csv data, queuing up to the server
	addresses, err := fileToAddressProtoArray(path)
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
			}).Error("/geocoder.Management/InsertorReplaceAddressData; failed stream.Send()")
		}
	}

	// get response back from server
	reply, err := stream.CloseAndRecv()
	if err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Error("/geocoder.Management/InsertorReplaceAddressData; failed recv")
	}

	log.WithFields(log.Fields{
		"insert.success":     reply.Success,
		"insert.num_objects": reply.TotalObjectsWritten,
	}).Info("/geocoder.Management/InsertorReplaceAddressData; success")

}

func main() {
	flag.Parse()

	// Init client and insert test data
	managementConn := srv.MustRPCClient(*rpcServerHost, *rpcServerPort)
	managementClient := pb.NewManagementClient(managementConn)

	// write the target file to the redis instance
	writeAddressData(managementClient, *targetFile)
}
