package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/encrypt"
)

// StorageService handles document storage operations
type StorageService struct {
	client     *minio.Client
	bucketName string
}

// NewStorageService creates a new storage service
func NewStorageService() (*StorageService, error) {
	endpoint := os.Getenv("STORAGE_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:9000"
	}

	accessKey := os.Getenv("STORAGE_ACCESS_KEY")
	if accessKey == "" {
		return nil, fmt.Errorf("STORAGE_ACCESS_KEY not configured")
	}

	secretKey := os.Getenv("STORAGE_SECRET_KEY")
	if secretKey == "" {
		return nil, fmt.Errorf("STORAGE_SECRET_KEY not configured")
	}

	bucketName := os.Getenv("STORAGE_BUCKET")
	if bucketName == "" {
		bucketName = "user-documents"
	}

	useSSL := os.Getenv("STORAGE_USE_SSL") == "true"

	// Initialize MinIO client
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}

	service := &StorageService{
		client:     client,
		bucketName: bucketName,
	}

	// Ensure bucket exists
	if err := service.ensureBucket(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ensure bucket: %w", err)
	}

	return service, nil
}

// ensureBucket creates the bucket if it doesn't exist
func (s *StorageService) ensureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucketName)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !exists {
		err = s.client.MakeBucket(ctx, s.bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
	}

	return nil
}

// UploadFile uploads a file to storage
func (s *StorageService) UploadFile(ctx context.Context, userID, fileName string, reader io.Reader, fileSize int64, contentType string) (string, error) {
	// Generate storage path: users/{userID}/documents/{timestamp}_{fileName}
	timestamp := time.Now().Unix()
	objectName := fmt.Sprintf("users/%s/documents/%d_%s", userID, timestamp, filepath.Base(fileName))

	// Upload file with server-side encryption
	_, err := s.client.PutObject(ctx, s.bucketName, objectName, reader, fileSize, minio.PutObjectOptions{
		ContentType:          contentType,
		ServerSideEncryption: encrypt.NewSSE(),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	return objectName, nil
}

// GetFile retrieves a file from storage
func (s *StorageService) GetFile(ctx context.Context, objectName string) (io.ReadCloser, error) {
	object, err := s.client.GetObject(ctx, s.bucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get file: %w", err)
	}

	return object, nil
}

// GetFileBytes retrieves file as bytes
func (s *StorageService) GetFileBytes(ctx context.Context, objectName string) ([]byte, error) {
	reader, err := s.GetFile(ctx, objectName)
	if err != nil {
		return nil, err
	}
	defer func(reader io.ReadCloser) {
		if err := reader.Close(); err != nil {
			log.Printf("failed to close reader: %v", err)
		}
	}(reader)

	var buffer bytes.Buffer
	if _, err := io.Copy(&buffer, reader); err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return buffer.Bytes(), nil
}

// DeleteFile deletes a file from storage
func (s *StorageService) DeleteFile(ctx context.Context, objectName string) error {
	err := s.client.RemoveObject(ctx, s.bucketName, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}

// GetFileInfo retrieves file metadata
func (s *StorageService) GetFileInfo(ctx context.Context, objectName string) (*minio.ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.bucketName, objectName, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	return &info, nil
}

// GeneratePresignedURL generates a temporary download URL
func (s *StorageService) GeneratePresignedURL(ctx context.Context, objectName string, expiry time.Duration) (string, error) {
	url, err := s.client.PresignedGetObject(ctx, s.bucketName, objectName, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return url.String(), nil
}

// ListUserDocuments lists all documents for a user
func (s *StorageService) ListUserDocuments(ctx context.Context, userID string) ([]minio.ObjectInfo, error) {
	prefix := fmt.Sprintf("users/%s/documents/", userID)
	objectsCh := s.client.ListObjects(ctx, s.bucketName, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	var objects []minio.ObjectInfo
	for object := range objectsCh {
		if object.Err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", object.Err)
		}
		objects = append(objects, object)
	}

	return objects, nil
}
