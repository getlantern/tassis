package attachments

// UploadAuthorization is an authorization to upload an attachment.
type UploadAuthorization struct {
	// The URL to which the attachment should be uploaded
	UploadURL string
	// When this authorization expires, as nanoseconds from the UNIX epoch
	AuthorizationExpiresAt int64
	// The URL from which this attachment can be downloaded once it has been uploaded
	DownloadURL string
}

type Manager interface {
	AuthorizeUpload() (*UploadAuthorization, error)
}
