package s3attachments

import (
	"bytes"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/getlantern/tassis/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testContent = "Hello I'm some test content"
	expiration  = 1 * time.Hour
)

func TestAuthorizeUpload(t *testing.T) {
	accessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	require.NotEmpty(t, accessKeyID, "need to specify AWS_ACCESS_KEY_ID environment variable in order to run this test")
	require.NotEmpty(t, secretAccessKey, "need to specify AWS_SECRET_ACCESS_KEY environment variable in order to run this test")

	m, err := NewManager(accessKeyID, secretAccessKey, "s3.us-east-1.wasabisys.com", "us-east-1", "tassis-test-us-east-1", expiration, len(testContent))
	require.NoError(t, err)

	require.EqualValues(t, len(testContent), m.MaxAttachmentSize())

	// authorize an upload
	auth, err := m.AuthorizeUpload()
	require.NoError(t, err)
	require.NotEmpty(t, auth.UploadURL)
	require.NotEmpty(t, auth.UploadFormData)
	require.EqualValues(t, len(testContent), auth.MaxUploadSize)
	expiresAt := util.TimeFromMillis(auth.AuthorizationExpiresAt)
	require.True(t, expiresAt.After(time.Now().Add(expiration).Add(-1*time.Hour)))
	require.True(t, expiresAt.Before(time.Now().Add(expiration)))
	require.NotEmpty(t, auth.DownloadURL)
	require.True(t, strings.Contains(auth.DownloadURL, folderForToday()))

	// upload some test content with the authorized upload URL
	buildUploadRequest := func(content string) *http.Request {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for key, value := range auth.UploadFormData {
			require.NoError(t, w.WriteField(key, value))
		}
		fw, err := w.CreateFormField("file")
		require.NoError(t, err)
		_, err = fw.Write([]byte(content))
		require.NoError(t, err)
		require.NoError(t, w.Close())
		req, err := http.NewRequest(http.MethodPost, auth.UploadURL, &buf)
		require.NoError(t, err)
		req.Header.Set("Content-Type", w.FormDataContentType())
		return req
	}

	client := http.Client{}
	resp, err := client.Do(buildUploadRequest(testContent))
	require.NoError(t, err)
	if !assert.Equal(t, http.StatusNoContent, resp.StatusCode) {
		resp.Header.Write(os.Stderr)
		io.Copy(os.Stderr, resp.Body)
		return
	}

	// download the test content
	checkDownload := func() {
		resp, err = client.Get(auth.DownloadURL)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		defer resp.Body.Close()
		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, testContent, string(b))
	}

	checkDownload()

	// try to upload something too big and make sure it fails
	resp, err = client.Do(buildUploadRequest(testContent + "a"))
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// download the test content again
	checkDownload()
}
