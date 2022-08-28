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
	// "google.golang.org/protobuf/reflect/protoreflect"
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

// GetBatchFromStorage
func GetBatchFromStorage(client *s3.S3, fileKey string, data proto.Message) error {

	// local
	if env := os.Getenv("ENVIRONMENT"); env == "LOCAL" {
		b, err := os.ReadFile(fmt.Sprintf("/tmp/%s", fileKey))
		if err != nil {
			return err
		}
		return json.Unmarshal(b, data)
	}

	// production && development case - write to S3/DO Spaces with real credentials
	var b []byte
	buf := bytes.NewBuffer(b)

	res, err := client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(batchServerStorageSpace),
		Key:    aws.String(fmt.Sprintf("%s/%s", batchServerStoragePrefix, fileKey)),
	})
	if err != nil {
		return err
	}

	io.Copy(buf, res.Body)
	return json.Unmarshal(b, data)
}

// persistBatchToStorage - writes a batch request to a storage medium
// on LOCAL -> local volume; on PROD/DEV -> DigitalOcean Spaces
func PersistBatchToStorage(client *s3.S3, data proto.Message, fileKey string) error {

	// marshall request into bytes - this isn't great for huge requests, but
	// don't expect more than 1MB for this service (famous last words, btw)
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// write to a local volume in the container //
	if env := os.Getenv("ENVIRONMENT"); env == "LOCAL" {
		return os.WriteFile(fmt.Sprintf("/tmp/%s", fileKey), b, 0644)
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
