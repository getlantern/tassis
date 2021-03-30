package s3attachments

import (
	"context"
	"time"

	"github.com/getlantern/golog"
	"github.com/getlantern/tassis/attachments"
	"github.com/getlantern/uuid"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	ONE_WEEK = 604800 * time.Second
)

var (
	log = golog.LoggerFor("s3attachments")
)

type manager struct {
	serviceURL        string
	bucket            string
	presignExpiration time.Duration
	client            *s3.Client
	presignClient     *s3.PresignClient
}

func New(serviceURL string, region string, bucket string, presignExpiration time.Duration) (attachments.Manager, error) {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, err
	}
	cfg.Region = region

	client := s3.NewFromConfig(cfg, s3.WithEndpointResolver(s3.EndpointResolverFromURL(serviceURL)))
	presignClient := s3.NewPresignClient(client, s3.WithPresignExpires(presignExpiration))

	return &manager{
		serviceURL:        serviceURL,
		bucket:            bucket,
		presignExpiration: presignExpiration,
		client:            client,
		presignClient:     presignClient,
	}, nil
}

func (m *manager) AuthorizeUpload() (*attachments.UploadAuthorization, error) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	defer cancel()

	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	key := id.String()

	expiration := time.Now().Add(m.presignExpiration)
	put, err := m.presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Key:    &key,
		Bucket: &m.bucket,
	}, s3.WithPresignExpires(m.presignExpiration))
	if err != nil {
		return nil, err
	}

	get, err := m.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Key:    &key,
		Bucket: &m.bucket,
	}, s3.WithPresignExpires(ONE_WEEK))
	if err != nil {
		return nil, err
	}

	return &attachments.UploadAuthorization{
		UploadURL:              put.URL,
		AuthorizationExpiresAt: int64(expiration.Add(-1 * time.Minute).UnixNano()),
		DownloadURL:            get.URL,
	}, nil
}
