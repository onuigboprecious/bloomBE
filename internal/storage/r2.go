package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type R2Service struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	BucketName      string
	PublicDomain    string
}

func NewR2ServiceFromEnv() *R2Service {
	accountID := os.Getenv("R2_ACCOUNT_ID")
	accessKeyID := os.Getenv("R2_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("R2_SECRET_ACCESS_KEY")
	bucketName := os.Getenv("R2_BUCKET_NAME")
	publicDomain := os.Getenv("R2_PUBLIC_DOMAIN")

	if publicDomain != "" {
		publicDomain = strings.TrimSuffix(publicDomain, "/")
	}

	return &R2Service{
		AccountID:       accountID,
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		BucketName:      bucketName,
		PublicDomain:    publicDomain,
	}
}

func (r *R2Service) IsConfigured() bool {
	return r.AccountID != "" && r.AccessKeyID != "" && r.SecretAccessKey != "" && r.BucketName != ""
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// UploadObject uploads binary file content directly to Cloudflare R2 using AWS SigV4
func (r *R2Service) UploadObject(ctx context.Context, objectKey string, fileData []byte, contentType string) (string, error) {
	if !r.IsConfigured() {
		return "", fmt.Errorf("Cloudflare R2 is not fully configured (R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, R2_BUCKET_NAME required)")
	}

	objectKey = strings.TrimPrefix(objectKey, "/")
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := "auto"
	service := "s3"

	host := fmt.Sprintf("%s.r2.cloudflarestorage.com", r.AccountID)
	endpoint := fmt.Sprintf("https://%s/%s/%s", host, r.BucketName, objectKey)

	payloadHash := sha256Hex(fileData)

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 1. Create Canonical Request
	canonicalURI := fmt.Sprintf("/%s/%s", r.BucketName, objectKey)
	canonicalQueryString := ""
	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n",
		contentType, host, payloadHash, amzDate)
	signedHeaders := "content-type;host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := fmt.Sprintf("PUT\n%s\n%s\n%s\n%s\n%s",
		canonicalURI, canonicalQueryString, canonicalHeaders, signedHeaders, payloadHash)

	// 2. Create String to Sign
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		amzDate, credentialScope, sha256Hex([]byte(canonicalRequest)))

	// 3. Calculate Signing Key
	kDate := hmacSHA256([]byte("AWS4"+r.SecretAccessKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")

	// 4. Calculate Signature
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	// 5. Construct Authorization Header
	authorization := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		r.AccessKeyID, credentialScope, signedHeaders, signature)

	// 6. Execute HTTP Request
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(fileData))
	if err != nil {
		return "", fmt.Errorf("failed to create R2 request: %w", err)
	}

	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", amzDate)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("R2 upload HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("R2 upload returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// 7. Return Public URL
	if r.PublicDomain != "" {
		return fmt.Sprintf("%s/%s", r.PublicDomain, objectKey), nil
	}

	return fmt.Sprintf("https://pub-%s.r2.dev/%s", r.AccountID, objectKey), nil
}

type R2ObjectItem struct {
	Key  string `json:"key"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// ListObjects fetches uploaded media objects from Cloudflare R2 bucket
func (r *R2Service) ListObjects(ctx context.Context, prefix string) ([]R2ObjectItem, error) {
	if !r.IsConfigured() {
		return nil, fmt.Errorf("Cloudflare R2 is not fully configured (R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, R2_BUCKET_NAME required)")
	}

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := "auto"
	service := "s3"

	host := fmt.Sprintf("%s.r2.cloudflarestorage.com", r.AccountID)
	query := "list-type=2"
	if prefix != "" {
		query += "&prefix=" + strings.TrimPrefix(prefix, "/")
	}

	endpoint := fmt.Sprintf("https://%s/%s?%s", host, r.BucketName, query)

	payloadHash := sha256Hex([]byte(""))
	canonicalURI := "/" + r.BucketName
	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n", host, payloadHash, amzDate)
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := fmt.Sprintf("GET\n%s\n%s\n%s\n%s\n%s", canonicalURI, query, canonicalHeaders, signedHeaders, payloadHash)

	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s", amzDate, credentialScope, sha256Hex([]byte(canonicalRequest)))

	kDate := hmacSHA256([]byte("AWS4"+r.SecretAccessKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")

	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	authorization := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		r.AccessKeyID, credentialScope, signedHeaders, signature)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", amzDate)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var listRes struct {
		Contents []struct {
			Key  string `xml:"Key"`
			Size int64  `xml:"Size"`
		} `xml:"Contents"`
	}

	if err := xml.Unmarshal(bodyBytes, &listRes); err != nil {
		return nil, err
	}

	var result []R2ObjectItem
	for _, c := range listRes.Contents {
		url := fmt.Sprintf("https://pub-%s.r2.dev/%s", r.AccountID, c.Key)
		if r.PublicDomain != "" {
			url = fmt.Sprintf("%s/%s", r.PublicDomain, c.Key)
		}
		result = append(result, R2ObjectItem{
			Key:  c.Key,
			URL:  url,
			Size: c.Size,
		})
	}
	return result, nil
}

// HandleUpload handles POST /api/upload multipart file uploads and GET /api/upload object listing
func (r *R2Service) HandleUpload(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodGet {
		items, err := r.ListObjects(req.Context(), req.URL.Query().Get("folder"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if items == nil {
			items = []R2ObjectItem{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "success",
			"count":  len(items),
			"images": items,
		})
		return
	}

	if req.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Limit upload to 10MB
	req.Body = http.MaxBytesReader(w, req.Body, 10<<20)
	if err := req.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "file size exceeds maximum limit of 10MB")
		return
	}

	file, header, err := req.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file payload is required")
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read uploaded file")
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".png"
	}

	folder := req.FormValue("folder")
	if folder == "" {
		folder = "avatars"
	}
	folder = strings.Trim(folder, "/")

	objectKey := fmt.Sprintf("%s/img_%d%s", folder, time.Now().UnixNano(), ext)
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(fileBytes)
	}

	if r.IsConfigured() {
		publicURL, err := r.UploadObject(req.Context(), objectKey, fileBytes, contentType)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"status": "success",
				"url":    publicURL,
				"key":    objectKey,
			})
			return
		}
		log.Printf("storage: r2 upload failed, falling back to data URL: %v", err)
	}

	// Seamless fallback if R2 credentials are not set on backend host
	base64Data := base64.StdEncoding.EncodeToString(fileBytes)
	dataURL := fmt.Sprintf("data:%s;base64,%s", contentType, base64Data)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "success",
		"url":    dataURL,
		"key":    objectKey,
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
