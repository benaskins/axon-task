package task

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

// ImageStore handles saving and loading images from the filesystem.
type ImageStore struct {
	dir string
}

// NewImageStore creates a store backed by the given directory.
func NewImageStore(dir string) (*ImageStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create image store directory: %w", err)
	}
	return &ImageStore{dir: dir}, nil
}

// validateImageID checks that an image ID does not contain path traversal characters.
func validateImageID(id string) error {
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return fmt.Errorf("invalid image ID")
	}
	return nil
}

// thumbnail size variants: suffix → max longest side in pixels.
var thumbVariants = []struct {
	suffix  string
	maxSide int
}{
	{"_thumb", 256},
	{"_medium", 512},
	{"_lg", 1024},
}

// SaveWithID writes image data to a file with the given ID, then generates
// thumbnail variants. Thumbnail failures are logged but don't fail the save.
func (s *ImageStore) SaveWithID(id string, data []byte) error {
	if err := validateImageID(id); err != nil {
		return err
	}
	path := filepath.Join(s.dir, id+".png")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	s.generateThumbnails(id, data)
	return nil
}

func (s *ImageStore) generateThumbnails(id string, data []byte) {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		slog.Warn("thumbnail decode failed", "id", id, "error", err)
		return
	}

	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()

	for _, v := range thumbVariants {
		outPath := filepath.Join(s.dir, id+v.suffix+".png")

		// If source is already at or below target size, copy the original.
		if srcW <= v.maxSide && srcH <= v.maxSide {
			if err := os.WriteFile(outPath, data, 0644); err != nil {
				slog.Warn("thumbnail copy failed", "id", id, "suffix", v.suffix, "error", err)
			}
			continue
		}

		// Scale preserving aspect ratio.
		newW, newH := fitDimensions(srcW, srcH, v.maxSide)
		dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)

		var buf bytes.Buffer
		if err := png.Encode(&buf, dst); err != nil {
			slog.Warn("thumbnail encode failed", "id", id, "suffix", v.suffix, "error", err)
			continue
		}
		if err := os.WriteFile(outPath, buf.Bytes(), 0644); err != nil {
			slog.Warn("thumbnail write failed", "id", id, "suffix", v.suffix, "error", err)
		}
	}
}

// fitDimensions returns width and height scaled so the longest side equals maxSide.
func fitDimensions(w, h, maxSide int) (int, int) {
	if w >= h {
		return maxSide, h * maxSide / w
	}
	return w * maxSide / h, maxSide
}

// BackfillThumbnails walks the image directory and generates missing thumbnails
// for all existing full-size images. Returns the number of images processed.
func (s *ImageStore) BackfillThumbnails() (int, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, fmt.Errorf("read image dir: %w", err)
	}

	count := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".png") {
			continue
		}
		// Skip variant files (contain _thumb, _medium, _lg before .png).
		base := strings.TrimSuffix(name, ".png")
		if strings.HasSuffix(base, "_thumb") || strings.HasSuffix(base, "_medium") || strings.HasSuffix(base, "_lg") {
			continue
		}

		// Check if all variants already exist.
		allExist := true
		for _, v := range thumbVariants {
			variantPath := filepath.Join(s.dir, base+v.suffix+".png")
			if _, err := os.Stat(variantPath); err != nil {
				allExist = false
				break
			}
		}
		if allExist {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			slog.Warn("backfill skip", "file", name, "error", err)
			continue
		}

		s.generateThumbnails(base, data)
		count++
		slog.Info("backfill: generated thumbnails", "id", base)
	}

	return count, nil
}

// Load reads image data by ID.
func (s *ImageStore) Load(id string) ([]byte, error) {
	if err := validateImageID(id); err != nil {
		return nil, err
	}

	path := filepath.Join(s.dir, id+".png")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("image not found: %w", err)
	}
	return data, nil
}
