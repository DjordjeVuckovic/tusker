package datapackage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type s3Store struct {
	client *s3.Client
	bucket string
	prefix string
}

func newS3Store(u *url.URL, cfg S3Config) (s3Store, string, error) {
	if u.Host == "" {
		return s3Store{}, "", fmt.Errorf("descriptor location %s names no bucket", u)
	}
	client, err := newS3Client(cfg)
	if err != nil {
		return s3Store{}, "", err
	}
	prefix, name := splitDescriptor(strings.TrimPrefix(u.Path, "/"))
	return s3Store{client: client, bucket: u.Host, prefix: prefix}, name, nil
}

// newS3Client builds the client from cfg alone. Ambient AWS config is never
// read, so a profile or AWS_* variable cannot sign a request meant for the
// dataset store. Without keys, requests go unsigned, which serves public
// buckets only.
func newS3Client(cfg S3Config) (*s3.Client, error) {
	var provider aws.CredentialsProvider = aws.AnonymousCredentials{}
	switch {
	case cfg.AccessKey != "" && cfg.SecretKey != "":
		provider = credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")
	case cfg.AccessKey != "":
		return nil, errors.New("DATASET_S3_SECRET_KEY is empty while DATASET_S3_ACCESS_KEY is set")
	case cfg.SecretKey != "":
		return nil, errors.New("DATASET_S3_ACCESS_KEY is empty while DATASET_S3_SECRET_KEY is set")
	}
	opts := s3.Options{Region: cfg.Region, Credentials: provider}
	if cfg.Endpoint != "" {
		opts.BaseEndpoint = aws.String(cfg.Endpoint)
		opts.UsePathStyle = true
	}
	if opts.Region == "" {
		opts.Region = defaultRegion(cfg.Endpoint)
	}
	return s3.New(opts), nil
}

// defaultRegion is "auto" for a custom endpoint, as R2 expects, and AWS's
// us-east-1 otherwise.
func defaultRegion(endpoint string) string {
	if endpoint != "" {
		return "auto"
	}
	return "us-east-1"
}

func (s s3Store) key(rel string) string { return path.Join(s.prefix, rel) }

func (s s3Store) location(rel string) string { return "s3://" + s.bucket + "/" + s.key(rel) }

func (s s3Store) open(ctx context.Context, rel string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(rel)),
	})
	if isS3NotFound(err) {
		return nil, &NotFoundError{Location: s.location(rel)}
	}
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", s.location(rel), err)
	}
	return out.Body, nil
}

func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NoSuchBucket", "NotFound":
			return true
		}
	}
	var respErr *awshttp.ResponseError
	return errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusNotFound
}
