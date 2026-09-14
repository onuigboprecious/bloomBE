package storage

import (
	"context"
	"testing"
)

func TestR2Upload(t *testing.T) {
	r := &R2Service{
		AccountID:       "4aa6acbaae801624488470399d0c3c37",
		AccessKeyID:     "0c3a21b6bc464a1d919b80c31e878013",
		SecretAccessKey: "cf252b643885aea41c20c0e50a0185a1cc88516c21fd8f9bd0eb99e584fd0530",
		BucketName:      "enlazer-media",
		PublicDomain:    "media.enlazer.cloud",
	}

	if !r.IsConfigured() {
		t.Fatalf("R2 service not configured")
	}

	testData := []byte("hello cloudflare r2 test upload")
	url, err := r.UploadObject(context.Background(), "test/hello.txt", testData, "text/plain")
	if err != nil {
		t.Fatalf("R2 upload error: %v", err)
	}

	t.Logf("R2 upload success: %s", url)
}
