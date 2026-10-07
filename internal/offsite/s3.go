package offsite

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/midrei/nullmodem-bbs/internal/config"
)

// S3 and the services speaking it, through minio-go (signing, regions,
// multipart uploads for big copies).

type s3Target struct {
	c      *minio.Client
	bucket string
	prefix string
}

func openS3(ctx context.Context, c config.OffsiteS3) (Target, error) {
	if c.Endpoint == "" || c.Bucket == "" || c.AccessKey == "" || c.SecretKey == "" {
		return nil, errors.New("offsite: S3 needs the endpoint, bucket, access key and secret key")
	}
	endpoint := strings.TrimSpace(c.Endpoint)
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	endpoint = strings.TrimRight(endpoint, "/")
	lookup := minio.BucketLookupAuto
	if c.PathStyle {
		lookup = minio.BucketLookupPath
	}
	cl, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure:       !c.Insecure,
		Region:       strings.TrimSpace(c.Region),
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("offsite: S3: %w", err)
	}
	t := &s3Target{c: cl, bucket: strings.TrimSpace(c.Bucket), prefix: strings.TrimLeft(strings.TrimSpace(c.Prefix), "/")}
	ok, err := cl.BucketExists(ctx, t.bucket)
	if err != nil {
		return nil, fmt.Errorf("offsite: S3: %w", explainS3(err))
	}
	if !ok {
		if err := cl.MakeBucket(ctx, t.bucket, minio.MakeBucketOptions{Region: c.Region}); err != nil {
			return nil, fmt.Errorf("offsite: S3: the bucket %s doesn't exist and couldn't be made: %w", t.bucket, explainS3(err))
		}
	}
	return t, nil
}

func explainS3(err error) error {
	resp := minio.ToErrorResponse(err)
	switch resp.Code {
	case "SignatureDoesNotMatch", "InvalidAccessKeyId":
		return fmt.Errorf("the access key or secret key is wrong (%s)", resp.Code)
	case "AccessDenied":
		return fmt.Errorf("the key may not do that (AccessDenied)")
	}
	return err
}

func (t *s3Target) Put(ctx context.Context, name string, r io.Reader, size int64) error {
	_, err := t.c.PutObject(ctx, t.bucket, t.prefix+name, r, size, minio.PutObjectOptions{ContentType: "application/octet-stream"})
	if err != nil {
		return fmt.Errorf("offsite: S3 upload: %w", explainS3(err))
	}
	return nil
}

func (t *s3Target) List(ctx context.Context) ([]string, error) {
	var out []string
	for o := range t.c.ListObjects(ctx, t.bucket, minio.ListObjectsOptions{Prefix: t.prefix + "nullmodem-"}) {
		if o.Err != nil {
			return nil, fmt.Errorf("offsite: S3 list: %w", explainS3(o.Err))
		}
		if n := strings.TrimPrefix(o.Key, t.prefix); ours(n) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (t *s3Target) Delete(ctx context.Context, name string) error {
	if err := t.c.RemoveObject(ctx, t.bucket, t.prefix+name, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("offsite: S3 delete: %w", explainS3(err))
	}
	return nil
}

func (t *s3Target) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	o, err := t.c.GetObject(ctx, t.bucket, t.prefix+name, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("offsite: S3 read: %w", explainS3(err))
	}
	return o, nil
}

func (t *s3Target) Close() error { return nil }
