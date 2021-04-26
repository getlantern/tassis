package testsupport

import (
	"errors"
	"time"

	"github.com/getlantern/tassis/attachments"
	"github.com/getlantern/tassis/model"
	"github.com/getlantern/tassis/util"
)

const (
	maxAttachmentSize = 5000000
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
			AuthorizationExpiresAt: util.UnixMillis(time.Now().Add(24 * time.Hour)),
			MaxUploadSize:          maxAttachmentSize,
			DownloadURL:            "downloadURL",
		}, nil
	} else {
		return nil, errors.New("random failure")
	}
}

func (m *noopAttachmentsManager) MaxAttachmentSize() int64 {
	return maxAttachmentSize
}
