package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	doubaoVideo2R2Prefix           = "doubao-video2/"
	doubaoVideo2R2DefaultTTL       = 24 * time.Hour
	doubaoVideo2R2MaximumTTL       = 48 * time.Hour
	doubaoVideo2R2UploadTTL        = 15 * time.Minute
	doubaoVideo2R2CompleteTTL      = 30 * time.Minute
	doubaoVideo2R2MaximumMediaSize = 64 << 20
)

var doubaoVideo2R2ObjectIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{32}\.[a-z0-9]{2,5}$`)

type doubaoVideo2R2Config struct {
	Endpoint    string
	AccessKeyID string
	SecretKey   string
	Bucket      string
	TTL         time.Duration
}

type DoubaoVideo2DirectUpload struct {
	ObjectID      string            `json:"object_id"`
	UploadURL     string            `json:"upload_url"`
	CompleteToken string            `json:"complete_token"`
	Method        string            `json:"method"`
	Headers       map[string]string `json:"headers"`
	ExpiresAt     int64             `json:"expires_at"`
}

type DoubaoVideo2CompletedUpload struct {
	MediaURL  string `json:"media_url"`
	ExpiresAt int64  `json:"expires_at"`
}

func DoubaoVideo2R2Configured() bool {
	return strings.TrimSpace(os.Getenv("DOUBAO_VIDEO2_R2_ENDPOINT")) != "" &&
		strings.TrimSpace(os.Getenv("DOUBAO_VIDEO2_R2_ACCESS_KEY_ID")) != "" &&
		strings.TrimSpace(os.Getenv("DOUBAO_VIDEO2_R2_SECRET_ACCESS_KEY")) != "" &&
		strings.TrimSpace(os.Getenv("DOUBAO_VIDEO2_R2_BUCKET")) != ""
}

func loadDoubaoVideo2R2Config() (doubaoVideo2R2Config, error) {
	config := doubaoVideo2R2Config{
		Endpoint:    strings.TrimRight(strings.TrimSpace(os.Getenv("DOUBAO_VIDEO2_R2_ENDPOINT")), "/"),
		AccessKeyID: strings.TrimSpace(os.Getenv("DOUBAO_VIDEO2_R2_ACCESS_KEY_ID")),
		SecretKey:   strings.TrimSpace(os.Getenv("DOUBAO_VIDEO2_R2_SECRET_ACCESS_KEY")),
		Bucket:      strings.TrimSpace(os.Getenv("DOUBAO_VIDEO2_R2_BUCKET")),
		TTL:         doubaoVideo2R2DefaultTTL,
	}
	if seconds := common.GetEnvOrDefault("DOUBAO_VIDEO2_R2_URL_TTL_SECONDS", int(doubaoVideo2R2DefaultTTL/time.Second)); seconds > 0 {
		config.TTL = time.Duration(seconds) * time.Second
	}
	if config.TTL > doubaoVideo2R2MaximumTTL {
		config.TTL = doubaoVideo2R2MaximumTTL
	}
	if config.Endpoint == "" || config.AccessKeyID == "" || config.SecretKey == "" || config.Bucket == "" {
		return doubaoVideo2R2Config{}, errors.New("DoubaoVideo2.0 R2 temporary media storage is not configured")
	}
	endpointURL, err := url.Parse(config.Endpoint)
	if err != nil || endpointURL.Host == "" || (endpointURL.Scheme != "https" && !isLoopbackHTTPURL(endpointURL)) {
		return doubaoVideo2R2Config{}, errors.New("DoubaoVideo2.0 R2 endpoint must be an HTTPS URL")
	}
	return config, nil
}

func isLoopbackHTTPURL(parsed *url.URL) bool {
	if parsed == nil || parsed.Scheme != "http" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// StoreDoubaoVideo2InlineMedia externalizes a data URL or raw Base64 media
// value into private R2 storage and returns a short-lived direct R2 URL.
func StoreDoubaoVideo2InlineMedia(ctx context.Context, source string) (string, error) {
	config, err := loadDoubaoVideo2R2Config()
	if err != nil {
		return "", err
	}
	data, contentType, extension, err := decodeDoubaoVideo2InlineMedia(source)
	if err != nil {
		return "", err
	}
	objectToken, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return "", fmt.Errorf("generate R2 object key: %w", err)
	}
	objectID := objectToken + "." + extension
	objectKey := doubaoVideo2R2Prefix + objectID
	request, err := newDoubaoVideo2R2Request(ctx, config, http.MethodPut, objectKey, data)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Cache-Control", "private, max-age=86400")
	response, err := (&http.Client{Timeout: 60 * time.Second}).Do(request)
	if err != nil {
		return "", fmt.Errorf("upload DoubaoVideo2.0 temporary media to R2: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("upload DoubaoVideo2.0 temporary media to R2 returned HTTP %d", response.StatusCode)
	}
	return presignDoubaoVideo2R2Object(ctx, config, objectKey)
}

// CreateDoubaoVideo2DirectUpload lets an authenticated client upload private
// media straight to R2. The upload must be completed and verified separately
// before the client receives a URL suitable for an upstream media reference.
func CreateDoubaoVideo2DirectUpload(ctx context.Context, userID int, contentType string, contentLength int64, checksumSHA256 string) (*DoubaoVideo2DirectUpload, error) {
	config, err := loadDoubaoVideo2R2Config()
	if err != nil {
		return nil, err
	}
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	extension, ok := doubaoVideo2MediaExtension(contentType)
	if !ok {
		return nil, fmt.Errorf("media type %q is not supported", contentType)
	}
	if contentLength <= 0 || contentLength > doubaoVideo2R2MaximumMediaSize {
		return nil, fmt.Errorf("content_length must be between 1 and %d bytes", doubaoVideo2R2MaximumMediaSize)
	}
	checksumBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(checksumSHA256))
	if err != nil || len(checksumBytes) != sha256.Size {
		return nil, errors.New("checksum_sha256 must be the Base64-encoded SHA-256 digest of the file")
	}
	objectToken, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return nil, fmt.Errorf("generate R2 object key: %w", err)
	}
	objectID := objectToken + "." + extension
	objectKey := doubaoVideo2R2Prefix + objectID
	s3Client := s3.New(s3.Options{
		Region: "auto", BaseEndpoint: aws.String(config.Endpoint), UsePathStyle: true,
		Credentials: credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.SecretKey, ""),
	})
	presigned, err := s3.NewPresignClient(s3Client).PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(config.Bucket), Key: aws.String(objectKey), ContentLength: aws.Int64(contentLength),
		ContentType: aws.String(contentType), ChecksumSHA256: aws.String(checksumSHA256),
	}, func(options *s3.PresignOptions) {
		options.Expires = doubaoVideo2R2UploadTTL
	})
	if err != nil {
		return nil, fmt.Errorf("presign DoubaoVideo2.0 R2 upload: %w", err)
	}
	headers := map[string]string{
		"Content-Type": contentType,
	}
	for key, values := range presigned.SignedHeader {
		if strings.EqualFold(key, "host") || strings.EqualFold(key, "content-length") || len(values) == 0 {
			continue
		}
		headers[key] = values[0]
	}
	completeExpires := time.Now().Add(doubaoVideo2R2CompleteTTL).Unix()
	completeToken := strconv.FormatInt(completeExpires, 10) + "." + common.GenerateHMAC(fmt.Sprintf("doubao-video2-r2-complete\n%d\n%s\n%d", userID, objectID, completeExpires))
	return &DoubaoVideo2DirectUpload{
		ObjectID: objectID, UploadURL: presigned.URL, CompleteToken: completeToken,
		Method: http.MethodPut, Headers: headers, ExpiresAt: time.Now().Add(doubaoVideo2R2UploadTTL).Unix(),
	}, nil
}

func CompleteDoubaoVideo2DirectUpload(ctx context.Context, userID int, objectID, completeToken string) (*DoubaoVideo2CompletedUpload, error) {
	if !doubaoVideo2R2ObjectIDPattern.MatchString(objectID) {
		return nil, errors.New("object_id is invalid")
	}
	parts := strings.Split(completeToken, ".")
	if len(parts) != 2 {
		return nil, errors.New("complete_token is invalid")
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || expires < time.Now().Unix() || expires > time.Now().Add(doubaoVideo2R2CompleteTTL).Unix()+60 {
		return nil, errors.New("complete_token is expired or invalid")
	}
	expected := common.GenerateHMAC(fmt.Sprintf("doubao-video2-r2-complete\n%d\n%s\n%d", userID, objectID, expires))
	if !strings.EqualFold(parts[1], expected) {
		return nil, errors.New("complete_token is invalid")
	}
	config, err := loadDoubaoVideo2R2Config()
	if err != nil {
		return nil, err
	}
	objectKey := doubaoVideo2R2Prefix + objectID
	request, err := newDoubaoVideo2R2Request(ctx, config, http.MethodHead, objectKey, nil)
	if err != nil {
		return nil, err
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return nil, fmt.Errorf("verify DoubaoVideo2.0 R2 upload: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("temporary R2 media is not available (HTTP %d)", response.StatusCode)
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	extension, supported := doubaoVideo2MediaExtension(contentType)
	validSize := response.ContentLength > 0 && response.ContentLength <= doubaoVideo2R2MaximumMediaSize
	validType := supported && strings.HasSuffix(objectID, "."+extension)
	if !validSize || !validType {
		_ = deleteDoubaoVideo2R2Object(ctx, config, objectKey)
		return nil, errors.New("uploaded media has an invalid size or content type")
	}
	mediaURL, err := presignDoubaoVideo2R2Object(ctx, config, objectKey)
	if err != nil {
		return nil, err
	}
	return &DoubaoVideo2CompletedUpload{MediaURL: mediaURL, ExpiresAt: time.Now().Add(config.TTL).Unix()}, nil
}

func deleteDoubaoVideo2R2Object(ctx context.Context, config doubaoVideo2R2Config, objectKey string) error {
	request, err := newDoubaoVideo2R2Request(ctx, config, http.MethodDelete, objectKey, nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("delete R2 object returned HTTP %d", response.StatusCode)
	}
	return nil
}

func decodeDoubaoVideo2InlineMedia(source string) ([]byte, string, string, error) {
	source = strings.TrimSpace(source)
	contentType := ""
	encoded := source
	if strings.HasPrefix(strings.ToLower(source), "data:") {
		comma := strings.IndexByte(source, ',')
		if comma <= len("data:") {
			return nil, "", "", errors.New("inline media data URL is malformed")
		}
		header := source[len("data:"):comma]
		parts := strings.Split(header, ";")
		contentType = strings.ToLower(strings.TrimSpace(parts[0]))
		base64Encoded := false
		for _, part := range parts[1:] {
			if strings.EqualFold(strings.TrimSpace(part), "base64") {
				base64Encoded = true
				break
			}
		}
		if !base64Encoded {
			return nil, "", "", errors.New("inline media data URL must use Base64 encoding")
		}
		encoded = source[comma+1:]
	} else if strings.HasPrefix(strings.ToLower(source), "base64,") {
		encoded = source[len("base64,"):]
	}
	encoded = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' || r == ' ' {
			return -1
		}
		return r
	}, encoded)
	if base64.StdEncoding.DecodedLen(len(encoded)) > doubaoVideo2R2MaximumMediaSize {
		return nil, "", "", fmt.Errorf("inline media exceeds the %d-byte temporary storage limit", doubaoVideo2R2MaximumMediaSize)
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		return nil, "", "", fmt.Errorf("decode inline media Base64: %w", err)
	}
	if len(data) == 0 || len(data) > doubaoVideo2R2MaximumMediaSize {
		return nil, "", "", errors.New("inline media is empty or exceeds the temporary storage limit")
	}
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = strings.ToLower(strings.TrimSpace(http.DetectContentType(data)))
	}
	extension, ok := doubaoVideo2MediaExtension(contentType)
	if !ok {
		return nil, "", "", fmt.Errorf("inline media type %q is not supported for temporary R2 storage", contentType)
	}
	return data, contentType, extension, nil
}

func doubaoVideo2MediaExtension(contentType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/jpeg", "image/jpg":
		return "jpg", true
	case "image/png":
		return "png", true
	case "image/webp":
		return "webp", true
	case "image/gif":
		return "gif", true
	case "video/mp4":
		return "mp4", true
	case "video/webm":
		return "webm", true
	case "audio/mpeg", "audio/mp3":
		return "mp3", true
	case "audio/wav", "audio/x-wav":
		return "wav", true
	case "audio/mp4", "audio/x-m4a":
		return "m4a", true
	default:
		return "", false
	}
}

func newDoubaoVideo2R2Request(ctx context.Context, config doubaoVideo2R2Config, method, objectKey string, data []byte) (*http.Request, error) {
	target := doubaoVideo2R2ObjectURL(config, objectKey)
	var body io.Reader
	if data != nil {
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	payloadHash := hex.EncodeToString(digest[:])
	request.Header.Set("x-amz-content-sha256", payloadHash)
	credentials := aws.Credentials{AccessKeyID: config.AccessKeyID, SecretAccessKey: config.SecretKey}
	if err := v4.NewSigner().SignHTTP(ctx, credentials, request, payloadHash, "s3", "auto", time.Now()); err != nil {
		return nil, fmt.Errorf("sign DoubaoVideo2.0 R2 request: %w", err)
	}
	return request, nil
}

func presignDoubaoVideo2R2Object(ctx context.Context, config doubaoVideo2R2Config, objectKey string) (string, error) {
	target := doubaoVideo2R2ObjectURL(config, objectKey)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	query := request.URL.Query()
	query.Set("X-Amz-Expires", strconv.FormatInt(int64(config.TTL/time.Second), 10))
	request.URL.RawQuery = query.Encode()
	credentials := aws.Credentials{AccessKeyID: config.AccessKeyID, SecretAccessKey: config.SecretKey}
	signedURL, _, err := v4.NewSigner().PresignHTTP(ctx, credentials, request, "UNSIGNED-PAYLOAD", "s3", "auto", time.Now())
	if err != nil {
		return "", fmt.Errorf("presign DoubaoVideo2.0 R2 object: %w", err)
	}
	return signedURL, nil
}

func doubaoVideo2R2ObjectURL(config doubaoVideo2R2Config, objectKey string) string {
	return config.Endpoint + "/" + url.PathEscape(config.Bucket) + "/" + strings.TrimLeft(objectKey, "/")
}
