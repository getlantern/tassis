package s3attachments

import (
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAuthorizeUpload(t *testing.T) {
	require.NotEmpty(t, os.Getenv("AWS_ACCESS_KEY_ID"), "need to specify AWS_ACCESS_KEY_ID environment variable in order to run this test")
	require.NotEmpty(t, os.Getenv("AWS_SECRET_ACCESS_KEY"), "need to specify AWS_SECRET_ACCESS_KEY environment variable in order to run this test")

	m, err := New("https://s3.eu-central-1.wasabisys.com", "eu-central-1", "tassis-eu-central-1", 15*time.Minute)
	require.NoError(t, err)

	// authorize an upload
	auth, err := m.AuthorizeUpload()
	require.NoError(t, err)

	// upload some test content with the authorized upload URL
	testContent := "Hello I'm some test content"
	client := http.Client{}
	req, err := http.NewRequest(http.MethodPut, auth.UploadURL, strings.NewReader(testContent))
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// download the test content
	resp, err = client.Get(auth.DownloadURL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, testContent, string(b))
}
