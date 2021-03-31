package testsupport

import (
	"errors"
	"time"

	"github.com/getlantern/tassis/attachments"
	"github.com/getlantern/tassis/model"
)

type noopAttachmentsManager struct {
	count int
}

func NewNoopAttachmentsManager() attachments.Manager {
	return &noopAttachmentsManager{}
}

func (m *noopAttachmentsManager) AuthorizeUpload() (*model.UploadAuthorization, error) {
	m.count++
	if m.count%2 == 1 {
		return &model.UploadAuthorization{
			UploadURL:              "uploadURL",
			UploadFormData:         map[string]string{"a": "a"},
			AuthorizationExpiresAt: time.Now().Add(24 * time.Hour).UnixNano(),
			MaxUploadSize:          5000000,
			DownloadURL:            "downloadURL",
		}, nil
	} else {
		return nil, errors.New("random failure")
	}
}
