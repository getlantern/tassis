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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testContent = "Hello I'm some test content"
)

func TestAuthorizeUpload(t *testing.T) {
	accessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	require.NotEmpty(t, accessKeyID, "need to specify AWS_ACCESS_KEY_ID environment variable in order to run this test")
	require.NotEmpty(t, secretAccessKey, "need to specify AWS_SECRET_ACCESS_KEY environment variable in order to run this test")

	m, err := New(accessKeyID, secretAccessKey, "s3.eu-central-1.wasabisys.com", "eu-central-1", "tassis-eu-central-1", 24*time.Hour, len(testContent))
	require.NoError(t, err)

	// authorize an upload
	auth, err := m.AuthorizeUpload()
	require.NoError(t, err)
	require.NotEmpty(t, auth.UploadURL)
	require.NotEmpty(t, auth.UploadFormData)
	require.EqualValues(t, len(testContent), auth.MaxUploadSize)
	expiration := time.Unix(0, auth.AuthorizationExpiresAt)
	require.True(t, expiration.After(time.Now().Add(23*time.Hour)))
	require.True(t, expiration.Before(time.Now().Add(24*time.Hour)))
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
