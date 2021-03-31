package attachments

import (
	"github.com/getlantern/tassis/model"
)

type Manager interface {
	AuthorizeUpload() (*model.UploadAuthorization, error)
}
