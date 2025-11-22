package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

func getAssetPath(mediaType string) string {
	ext := mediaTypeToExt(mediaType)

	// generate random ID for the filename so it is new each upload
	key := make([]byte, 32)
	_, err := rand.Read(key)
	if err != nil {
		panic("failed to generate random bytes")
	}
	randomFileName := base64.RawURLEncoding.EncodeToString(key)

	return fmt.Sprintf("%s%s", randomFileName, ext)
}

func (cfg apiConfig) getObjectURL(key string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, key)
}

func (cfg apiConfig) getAssetDiskPath(assetPath string) string {
	return filepath.Join(cfg.assetsRoot, assetPath)
}

func (cfg apiConfig) getAssetURL(assetPath string) string {
	return fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, assetPath)
}

func mediaTypeToExt(mediaType string) string {
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 {
		return ".bin"
	}
	return "." + parts[1]
}

func getVideoAspectRatio(filePath string) (string, error) {
	var b bytes.Buffer
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	cmd.Stdout = &b

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("ffprobe error: %v", err)
	}

	var videoMetadata VideoMetadata
	err = json.Unmarshal(b.Bytes(), &videoMetadata)
	if err != nil {
		return "", fmt.Errorf("could not parse ffprobe output: %v", err)
	}

	if len(videoMetadata.Streams) == 0 {
		return "", errors.New("no video streams found")
	}

	width := videoMetadata.Streams[0].Width
	height := videoMetadata.Streams[0].Height

	aspectRatio := float64(width) / float64(height)
	if aspectRatio >= 1.776 && aspectRatio <= 1.778 {
		return "16:9", nil
	} else if aspectRatio >= 0.562 && aspectRatio <= 0.563 {
		return "9:16", nil
	}
	return "other", nil
}
