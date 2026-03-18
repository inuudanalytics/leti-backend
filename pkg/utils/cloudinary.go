package utils

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
)

type CloudinaryService struct {
	cld *cloudinary.Cloudinary
}

type UploadFile struct {
	Reader   io.Reader
	Filename string
}

var allowedImageExts = []string{".jpg", ".jpeg", ".png", ".webp", ".heic", ".heif"}

func InitCloudinary() (*CloudinaryService, error) {
	cld, err := cloudinary.NewFromParams(
		os.Getenv("CLOUDINARY_CLOUD_NAME"),
		os.Getenv("CLOUDINARY_API_KEY"),
		os.Getenv("CLOUDINARY_API_SECRET"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Cloudinary: %v", err)
	}
	return &CloudinaryService{cld: cld}, nil
}

func (c *CloudinaryService) GetCloudinary() *cloudinary.Cloudinary {
	return c.cld
}

func IsAllowedImageExt(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return slices.Contains(allowedImageExts, ext)
}

// UploadImages uploads 1 or more images to the given folder.
// Returns a slice of secure URLs in the same order as the input files.
// maxFiles = 0 means no limit.
func (c *CloudinaryService) UploadImages(ctx context.Context, files []UploadFile, folder string, maxFiles int) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if maxFiles > 0 && len(files) > maxFiles {
		return nil, fmt.Errorf("a maximum of %d image(s) can be uploaded", maxFiles)
	}
	if folder == "" {
		folder = "general"
	}

	var urls []string
	for _, f := range files {
		if !IsAllowedImageExt(f.Filename) {
			return nil, fmt.Errorf("invalid image format for %q: allowed formats are jpg, jpeg, png, webp, heic, heif", f.Filename)
		}

		res, err := c.cld.Upload.Upload(ctx, f.Reader, uploader.UploadParams{
			Folder:       folder,
			ResourceType: "image",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to upload %q: %w", f.Filename, err)
		}

		urls = append(urls, res.SecureURL)
	}

	return urls, nil
}

// DeleteImage deletes an image by its public ID.
func (c *CloudinaryService) DeleteImage(ctx context.Context, publicID string) error {
	_, err := c.cld.Upload.Destroy(ctx, uploader.DestroyParams{
		PublicID:     publicID,
		ResourceType: "image",
	})
	if err != nil {
		return fmt.Errorf("failed to delete image %q: %w", publicID, err)
	}
	return nil
}
