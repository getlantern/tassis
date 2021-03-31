package s3attachments

import (
	"context"
	"fmt"
	"time"

	"github.com/getlantern/errors"
	"github.com/getlantern/golog"
	"github.com/getlantern/tassis/attachments"
	"github.com/getlantern/tassis/model"
	"github.com/getlantern/uuid"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	ONE_WEEK = 604800 * time.Second
)

const (
	bucketPolicyTemplate = `
{
	"Id": "PolicyAllowPublicDownloadFrom...%s-%s",
	"Version": "2012-10-17",
	"Statement": [
		{
		"Sid": "ThisAutoGeneratedPolicyAllowsPublicDownloadingOfObjectsFromThisBucket",
		"Action": [
			"s3:GetObject"
		],
		"Effect": "Allow",
		"Resource": "arn:aws:s3:::%s/*",
		"Principal": "*"
		}
	]
}
`
)

var (
	log = golog.LoggerFor("s3attachments")
)

type manager struct {
	serviceURL        string
	bucket            string
	presignExpiration time.Duration
	maxContentLength  int64
	client            *minio.Client
}

func New(accessKeyID, secretAccessKey, endpoint, region, bucket string, presignExpiration time.Duration, maxContentLength int) (attachments.Manager, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: true,
	})
	if err != nil {
		return nil, errors.New("unable to build S3 client: %v", err)
	}

	// set bucket policy to allow public downloads`
	policyID, err := uuid.NewRandom()
	if err != nil {
		return nil, errors.New("unable to generate random policy id: %v", err)
	}
	bucketPolicy := fmt.Sprintf(bucketPolicyTemplate, bucket, policyID, bucket)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(1*time.Minute))
	defer cancel()
	err = client.SetBucketPolicy(ctx, bucket, bucketPolicy)
	if err != nil {
		return nil, errors.New("unable to set bucket policy to allow public downloads: %v", err)
	}

	return &manager{
		serviceURL:        fmt.Sprintf("https://%s", endpoint),
		bucket:            bucket,
		presignExpiration: presignExpiration,
		maxContentLength:  int64(maxContentLength),
		client:            client,
	}, nil
}

func (m *manager) AuthorizeUpload() (*model.UploadAuthorization, error) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	defer cancel()

	id, err := uuid.NewRandom()
	if err != nil {
		return nil, errors.New("unable to generate random file id: %v", err)
	}
	key := id.String()

	expiration := time.Now().Add(m.presignExpiration)
	policy := minio.NewPostPolicy()
	policy.SetKey(key)
	policy.SetBucket(m.bucket)
	policy.SetExpires(expiration.Add(1 * time.Minute))
	policy.SetContentLengthRange(1, m.maxContentLength)

	u, formData, err := m.client.PresignedPostPolicy(ctx, policy)
	if err != nil {
		return nil, errors.New("unable to generate presigned POST request: %v", err)
	}

	return &model.UploadAuthorization{
		UploadURL:              u.String(),
		UploadFormData:         formData,
		AuthorizationExpiresAt: int64(expiration.Add(-1 * time.Minute).UnixNano()),
		MaxUploadSize:          m.maxContentLength,
		DownloadURL:            fmt.Sprintf("%v/%v/%v", m.serviceURL, m.bucket, key),
	}, nil
}
