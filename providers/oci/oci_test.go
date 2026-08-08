package oci

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/efficientgo/core/testutil"
	"github.com/go-kit/log"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/objectstorage"
	"github.com/oracle/oci-go-sdk/v65/objectstorage/transfer"
	"github.com/pkg/errors"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/errutil"
	"gopkg.in/yaml.v2"
)

func TestNewBucketWithErrorRoundTripper(t *testing.T) {
	const mockPrivateKey = `-----BEGIN RSA PRIVATE KEY-----
MIICXgIBAAKBgQDCFENGw33yGihy92pDjZQhl0C36rPJj+CvfSC8+q28hxA161QF
NUd13wuCTUcq0Qd2qsBe/2hFyc2DCJJg0h1L78+6Z4UMR7EOcpfdUE9Hf3m/hs+F
UR45uBJeDK1HSFHD8bHKD6kv8FPGfJTotc+2xjJwoYi+1hqp1fIekaxsyQIDAQAB
AoGBAJR8ZkCUvx5kzv+utdl7T5MnordT1TvoXXJGXK7ZZ+UuvMNUCdN2QPc4sBiA
QWvLw1cSKt5DsKZ8UETpYPy8pPYnnDEz2dDYiaew9+xEpubyeW2oH4Zx71wqBtOK
kqwrXa/pzdpiucRRjk6vE6YY7EBBs/g7uanVpGibOVAEsqH1AkEA7DkjVH28WDUg
f1nqvfn2Kj6CT7nIcE3jGJsZZ7zlZmBmHFDONMLUrXR/Zm3pR5m0tCmBqa5RK95u
412jt1dPIwJBANJT3v8pnkth48bQo/fKel6uEYyboRtA5/uHuHkZ6FQF7OUkGogc
mSJluOdc5t6hI1VsLn0QZEjQZMEOWr+wKSMCQQCC4kXJEsHAve77oP6HtG/IiEn7
kpyUXRNvFsDE0czpJJBvL/aRFUJxuRK91jhjC68sA7NsKMGg5OXb5I5Jj36xAkEA
gIT7aFOYBFwGgQAQkWNKLvySgKbAZRTeLBacpHMuQdl1DfdntvAyqpAZ0lY0RKmW
G6aFKaqQfOXKCyWoUiVknQJAXrlgySFci/2ueKlIE1QqIiLSZ8V8OlpFLRnb1pzI
7U1yQXnTAEFYM560yJlzUpOb1V4cScGd365tiSMvxLOvTA==
-----END RSA PRIVATE KEY-----`

	config := DefaultConfig
	config.Provider = "raw"
	config.Tenancy = "test"
	config.User = "test"
	config.Region = "test"
	config.Fingerprint = "123"
	config.PrivateKey = mockPrivateKey
	config.Passphrase = "123"
	ociConfig, err := yaml.Marshal(config)
	testutil.Ok(t, err)

	_, err = NewBucket(log.NewNopLogger(), ociConfig, errutil.WrapWithErrRoundtripper)
	// We expect an error from the RoundTripper
	testutil.NotOk(t, err)
	testutil.Assert(t, errutil.IsMockedError(err), "Expected RoundTripper error, got: %v", err)
}
func TestSupportedObjectUploadOptions(t *testing.T) {
	b := &Bucket{}

	testutil.Equals(
		t,
		[]objstore.ObjectUploadOptionType{
			objstore.ContentType,
			objstore.IfMatch,
			objstore.IfNotExists,
		},
		b.SupportedObjectUploadOptions(),
	)
}
func TestApplyUploadConditionsIfMatch(t *testing.T) {
	version := &objstore.ObjectVersion{
		Type:  objstore.ETag,
		Value: `"test-etag"`,
	}

	uploadOptions := objstore.ApplyObjectUploadOptions(
		objstore.WithIfMatch(version),
	)

	req := transfer.UploadRequest{}

	err := applyUploadConditions(&req, uploadOptions)

	testutil.Ok(t, err)
	testutil.Assert(t, req.IfMatch != nil)
	testutil.Equals(t, `"test-etag"`, *req.IfMatch)
	testutil.Assert(t, req.IfNoneMatch == nil)
}

func TestApplyUploadConditionsIfNotExists(t *testing.T) {
	uploadOptions := objstore.ApplyObjectUploadOptions(
		objstore.WithIfNotExists(),
	)

	req := transfer.UploadRequest{}

	err := applyUploadConditions(&req, uploadOptions)

	testutil.Ok(t, err)
	testutil.Assert(t, req.IfMatch == nil)
	testutil.Assert(t, req.IfNoneMatch != nil)
	testutil.Equals(t, "*", *req.IfNoneMatch)
}

func TestApplyUploadConditionsWithoutCondition(t *testing.T) {
	uploadOptions := objstore.ApplyObjectUploadOptions()

	req := transfer.UploadRequest{}

	err := applyUploadConditions(&req, uploadOptions)

	testutil.Ok(t, err)
	testutil.Assert(t, req.IfMatch == nil)
	testutil.Assert(t, req.IfNoneMatch == nil)
}

func TestApplyUploadConditionsRejectsGeneration(t *testing.T) {
	version := &objstore.ObjectVersion{
		Type:  objstore.Generation,
		Value: "123",
	}

	uploadOptions := objstore.ApplyObjectUploadOptions(
		objstore.WithIfMatch(version),
	)

	req := transfer.UploadRequest{}

	err := applyUploadConditions(&req, uploadOptions)

	testutil.Equals(t, errConditionInvalid, err)
	testutil.Assert(t, req.IfMatch == nil)
	testutil.Assert(t, req.IfNoneMatch == nil)
}

func TestAttributesFromHeadObjectResponse(t *testing.T) {
	lastModified := common.SDKTime{
		Time: time.Date(
			2026,
			time.August,
			4,
			12,
			0,
			0,
			0,
			time.UTC,
		),
	}

	response := objectstorage.HeadObjectResponse{
		ContentLength: common.Int64(1234),
		LastModified:  &lastModified,
		ETag:          common.String(`"oci-etag-value"`),
	}

	attrs := attributesFromHeadObjectResponse(response)

	testutil.Equals(t, int64(1234), attrs.Size)
	testutil.Equals(t, lastModified.Time, attrs.LastModified)

	testutil.Assert(t, attrs.Version != nil)
	testutil.Equals(t, objstore.ETag, attrs.Version.Type)
	testutil.Equals(t, `"oci-etag-value"`, attrs.Version.Value)
}
func TestAttributesFromHeadObjectResponseWithoutETag(t *testing.T) {
	response := objectstorage.HeadObjectResponse{
		ContentLength: common.Int64(10),
	}

	attrs := attributesFromHeadObjectResponse(response)

	testutil.Equals(t, int64(10), attrs.Size)
	testutil.Assert(t, attrs.Version == nil)
}

type fakeServiceError struct {
	statusCode int
}

func (e fakeServiceError) Error() string           { return "fake OCI service error" }
func (e fakeServiceError) GetHTTPStatusCode() int  { return e.statusCode }
func (e fakeServiceError) GetMessage() string      { return "fake" }
func (e fakeServiceError) GetCode() string         { return "Fake" }
func (e fakeServiceError) GetOpcRequestID() string { return "" }

func TestIsConditionNotMetErr(t *testing.T) {
	b := &Bucket{}

	testutil.Assert(t,
		b.IsConditionNotMetErr(fakeServiceError{statusCode: http.StatusPreconditionFailed}),
		"expected HTTP 412 to be classified as a condition failure",
	)
	testutil.Assert(t,
		!b.IsConditionNotMetErr(fakeServiceError{statusCode: http.StatusNotFound}),
		"expected HTTP 404 not to be classified as a condition failure",
	)
	testutil.Assert(t,
		b.IsConditionNotMetErr(errConditionInvalid),
		"expected an unsupported condition type to be classified as a condition failure",
	)
	testutil.Assert(t,
		!b.IsConditionNotMetErr(errors.New("unrelated error")),
		"expected an unrelated error not to be classified as a condition failure",
	)
}

var errConditionalReader = errors.New("reader failed")

type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) {
	return 0, errConditionalReader
}

func TestUploadConditionallyReaderError(t *testing.T) {
	b := &Bucket{}

	err := b.uploadConditionally(
		context.Background(),
		failingReader{},
		transfer.UploadRequest{},
	)

	testutil.NotOk(t, err)
	testutil.Assert(t, errors.Is(err, errConditionalReader),
		"expected the reader error to be preserved, got: %v", err)
}
