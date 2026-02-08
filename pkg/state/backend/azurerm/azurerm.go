// Package azurerm implements an Azure Blob Storage state backend.
package azurerm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/davidthor/cldctl/pkg/state/backend"
	"github.com/google/uuid"
)

func init() {
	backend.Register("azurerm", NewBackend)
}

// Backend implements the state backend interface for Azure Blob Storage.
type Backend struct {
	client        *azblob.Client
	containerName string
	prefix        string
}

// NewBackend creates a new Azure Blob Storage backend.
func NewBackend(cfg map[string]string) (backend.Backend, error) {
	storageAccount, ok := cfg["storage_account_name"]
	if !ok || storageAccount == "" {
		return nil, fmt.Errorf("azurerm backend requires 'storage_account_name' configuration")
	}

	containerName, ok := cfg["container_name"]
	if !ok || containerName == "" {
		return nil, fmt.Errorf("azurerm backend requires 'container_name' configuration")
	}

	var client *azblob.Client
	var err error

	// Build the service URL
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", storageAccount)

	// Support custom endpoint (for Azurite emulator)
	if endpoint := cfg["endpoint"]; endpoint != "" {
		serviceURL = endpoint
	}

	// Support explicit access key authentication
	if accessKey := cfg["access_key"]; accessKey != "" {
		cred, err := azblob.NewSharedKeyCredential(storageAccount, accessKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create shared key credential: %w", err)
		}
		client, err = azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure client with shared key: %w", err)
		}
	} else if sasToken := cfg["sas_token"]; sasToken != "" {
		// Support SAS token authentication
		var serviceURLWithSAS string
		if !strings.Contains(serviceURL, "?") {
			serviceURLWithSAS = serviceURL + "?" + strings.TrimPrefix(sasToken, "?")
		} else {
			serviceURLWithSAS = serviceURL + "&" + strings.TrimPrefix(sasToken, "?")
		}
		client, err = azblob.NewClientWithNoCredential(serviceURLWithSAS, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure client with SAS token: %w", err)
		}
	} else if connectionString := cfg["connection_string"]; connectionString != "" {
		// Support connection string authentication
		client, err = azblob.NewClientFromConnectionString(connectionString, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure client from connection string: %w", err)
		}
	} else {
		// Default to Azure Identity (DefaultAzureCredential)
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create default Azure credential: %w", err)
		}
		client, err = azblob.NewClient(serviceURL, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure client: %w", err)
		}
	}

	return &Backend{
		client:        client,
		containerName: containerName,
		prefix:        cfg["key"],
	}, nil
}

func (b *Backend) Type() string {
	return "azurerm"
}

func (b *Backend) Read(ctx context.Context, statePath string) (io.ReadCloser, error) {
	blobPath := b.fullPath(statePath)

	resp, err := b.client.DownloadStream(ctx, b.containerName, blobPath, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil, backend.ErrNotFound
		}
		return nil, fmt.Errorf("failed to read state from azure://%s/%s: %w", b.containerName, blobPath, err)
	}

	return resp.Body, nil
}

func (b *Backend) Write(ctx context.Context, statePath string, data io.Reader) error {
	blobPath := b.fullPath(statePath)

	// Read all data to get content
	content, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("failed to read data: %w", err)
	}

	_, err = b.client.UploadBuffer(ctx, b.containerName, blobPath, content, &azblob.UploadBufferOptions{
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: toPtr("application/json"),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to write state to azure://%s/%s: %w", b.containerName, blobPath, err)
	}

	return nil
}

func (b *Backend) Delete(ctx context.Context, statePath string) error {
	blobPath := b.fullPath(statePath)

	_, err := b.client.DeleteBlob(ctx, b.containerName, blobPath, nil)
	if err != nil {
		// Ignore not found errors for idempotency
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil
		}
		return fmt.Errorf("failed to delete state from azure://%s/%s: %w", b.containerName, blobPath, err)
	}

	return nil
}

func (b *Backend) List(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := b.fullPath(prefix)

	var paths []string
	pager := b.client.NewListBlobsFlatPager(b.containerName, &container.ListBlobsFlatOptions{
		Prefix: &fullPrefix,
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list blobs: %w", err)
		}

		for _, blob := range page.Segment.BlobItems {
			if blob.Name != nil {
				// Return path relative to backend prefix
				relPath := strings.TrimPrefix(*blob.Name, b.prefix+"/")
				if b.prefix == "" {
					relPath = *blob.Name
				}
				paths = append(paths, relPath)
			}
		}
	}

	return paths, nil
}

func (b *Backend) Exists(ctx context.Context, statePath string) (bool, error) {
	blobPath := b.fullPath(statePath)

	_, err := b.client.ServiceClient().NewContainerClient(b.containerName).NewBlobClient(blobPath).GetProperties(ctx, nil)
	if err != nil {
		var respErr *azcore.ResponseError
		if errors.As(err, &respErr) && respErr.StatusCode == 404 {
			return false, nil
		}
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check existence: %w", err)
	}

	return true, nil
}

func (b *Backend) Lock(ctx context.Context, statePath string, info backend.LockInfo) (backend.Lock, error) {
	lockPath := b.fullPath(statePath + ".lock")

	// Check for existing lock
	existingLock, err := b.readLock(ctx, lockPath)
	if err == nil {
		// Check if lock is stale (older than 1 hour)
		if time.Since(existingLock.Created) < time.Hour {
			return nil, &backend.LockError{
				Info: existingLock,
				Err:  backend.ErrLocked,
			}
		}
	}

	// Create lock
	info.ID = uuid.New().String()
	info.Path = statePath
	info.Created = time.Now()

	lockData, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal lock info: %w", err)
	}

	_, err = b.client.UploadBuffer(ctx, b.containerName, lockPath, lockData, &azblob.UploadBufferOptions{
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: toPtr("application/json"),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create lock: %w", err)
	}

	return &azureLock{
		backend: b,
		path:    lockPath,
		info:    info,
	}, nil
}

func (b *Backend) readLock(ctx context.Context, lockPath string) (backend.LockInfo, error) {
	resp, err := b.client.DownloadStream(ctx, b.containerName, lockPath, nil)
	if err != nil {
		return backend.LockInfo{}, err
	}
	defer resp.Body.Close()

	var info backend.LockInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return backend.LockInfo{}, err
	}

	return info, nil
}

func (b *Backend) fullPath(statePath string) string {
	if b.prefix == "" {
		return statePath
	}
	return path.Join(b.prefix, statePath)
}

// azureLock implements the Lock interface for Azure Blob Storage.
type azureLock struct {
	backend *Backend
	path    string
	info    backend.LockInfo
}

func (l *azureLock) ID() string {
	return l.info.ID
}

func (l *azureLock) Unlock(ctx context.Context) error {
	_, err := l.backend.client.DeleteBlob(ctx, l.backend.containerName, l.path, nil)
	if err != nil && !bloberror.HasCode(err, bloberror.BlobNotFound) {
		return fmt.Errorf("failed to release lock: %w", err)
	}
	return nil
}

func (l *azureLock) Info() backend.LockInfo {
	return l.info
}

// Ensure we implement the Backend interface
var _ backend.Backend = (*Backend)(nil)

// toPtr returns a pointer to the given value.
func toPtr[T any](v T) *T {
	return &v
}
