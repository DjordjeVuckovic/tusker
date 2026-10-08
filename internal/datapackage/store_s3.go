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
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type s3Store struct {
	client *s3.Client
	bucket string
	prefix string
}

func newS3Store(ctx context.Context, u *url.URL, cfg S3Config) (s3Store, string, error) {
	if u.Host == "" {
		return s3Store{}, "", fmt.Errorf("descriptor location %s names no bucket", u)
	}
	client, err := newS3Client(ctx, cfg)
	if err != nil {
		return s3Store{}, "", err
	}
	prefix, name := splitDescriptor(strings.TrimPrefix(u.Path, "/"))
	return s3Store{client: client, bucket: u.Host, prefix: prefix}, name, nil
}

func newS3Client(ctx context.Context, cfg S3Config) (*s3.Client, error) {
	var loadOpts []func(*awsconfig.LoadOptions) error
	if cfg.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load s3 config: %w", err)
	}
	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		}
		if o.Region == "" {
			o.Region = "auto"
		}
	}), nil
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
