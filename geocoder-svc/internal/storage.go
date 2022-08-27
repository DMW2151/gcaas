package srv

import (
	//  std library
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	// external
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"google.golang.org/protobuf/proto"
)

const (
	// batchServerStoragePrefix -
	batchServerStoragePrefix = "/datasets/original"

	// batchServerStorageSpace -
	batchServerStorageSpace = "gcaas-data-storage"

	// batchServerStorageEndPoint
	batchServerStorageEndPoint = "https://nyc3.digitaloceanspaces.com"

	// resultsAvailableDuration -
	resultsAvailableDuration = time.Minute * 60 * 24
)

// GeneratePresignedURL
func GeneratePresignedURL(client *s3.S3, fileKey string) (string, error) {
	req, _ := client.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(batchServerStorageSpace),
		Key:    aws.String(fmt.Sprintf("%s/%s", batchServerStoragePrefix, fileKey)),
	})
	return req.Presign(resultsAvailableDuration)
}

func GetBatchFromStorage(client *s3.S3, fileKey string, data proto.Message) error {
	res, err := client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(batchServerStorageSpace),
		Key:    aws.String(fmt.Sprintf("%s/%s", batchServerStoragePrefix, fileKey)),
	})
	if err != nil {
		return err
	}

	b := []byte{}
	buf := bytes.NewBuffer(b)

	// todo: not efficient - waste of alot of allocs - oh well; drop in the bucket...
	io.Copy(buf, res.Body)
	return proto.Unmarshal(b, data)
}

// persistBatchToStorage - writes a batch request to a storage medium
// on LOCAL -> local volume; on PROD/DEV -> DigitalOcean Spaces
func PersistBatchToStorage(client *s3.S3, data any, fileKey string) error {

	// check env var exists
	env := os.Getenv("ENVIRONMENT")

	// marshall request into bytes - this isn't great for huge requests, but
	// don't expect more than 1MB for this service (famous last words, btw)
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// write to a local volume in the container //
	if env == "LOCAL" {
		f, err := os.Create(fmt.Sprintf("/tmp/%s", fileKey))
		if err != nil {
			return err
		}

		defer f.Close()
		f.Write(b)
		return nil
	}

	// write to DigitalOcean //
	_, err = client.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(batchServerStorageSpace),
		Key:    aws.String(fmt.Sprintf("%s/%s", batchServerStoragePrefix, fileKey)),
		Body:   bytes.NewReader(b),
		ACL:    aws.String("private"),
	})
	if err != nil {
		return err
	}
	return nil
}
