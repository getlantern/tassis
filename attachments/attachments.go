package attachments

import (
	"github.com/getlantern/tassis/model"
)

// Manager is an interface for a facility that can authorize uploads to an attachment storage service like S3.
type Manager interface {
	// AuthorizeUpload authorizes an upload. The returned authorization will include URLs for uploading
	// and downloading the attachment. Uploads are expected to be submitted as a multipart form post that
	// includes the UploadFormData from the authorization as well as a "file" field with the file itself.
	// Upload authorizations are time limited and expire approximately at the AuthorizationExpiresAt listed
	// in the authorization. Uploads are also limited to a content size indicated by MaxUploadSize.
	AuthorizeUpload() (*model.UploadAuthorization, error)

	// MaxAttachmentSize reports the maximum attachment size supported by this upload manager.
	MaxAttachmentSize() int64
}
