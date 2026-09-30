package embedding

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

// Source selects how document embeddings are produced.
type Source string

const (
	// SourceOnline generates embeddings inline during ingestion via Ollama.
	SourceOnline Source = "online"
	// SourceFile loads precomputed embeddings from an object store (datapipe load embeddings).
	SourceFile Source = "file"
	// SourceNone disables embeddings entirely.
	SourceNone Source = "none"
)

// ObjectStoreConfig describes where the precomputed embeddings file lives.
// Used when Source == SourceFile.
type ObjectStoreConfig struct {
	Endpoint     string
	Region       string
	Bucket       string
	Key          string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
	// LocalPath, when set, bypasses the object store and reads a local file.
	LocalPath string
}

type Config struct {
	Enabled     bool
	Source      Source
	Model       string
	MaxLength   *int
	BaseURL     string
	ObjectStore ObjectStoreConfig
}

func LoadConfigFromEnv() (*Config, error) {
	enabled := os.Getenv("EMBEDDING_ENABLED")
	model := os.Getenv("EMBEDDING_MODEL")
	maxLen := os.Getenv("EMBEDDING_MAX_LENGTH")
	baseUrl := os.Getenv("EMBEDDING_BASE_URL")

	source := Source(os.Getenv("EMBEDDING_SOURCE"))
	if source == "" {
		source = SourceOnline
	}

	// The Ollama base URL is only needed for online generation.
	if source == SourceOnline && enabled == "true" && baseUrl == "" {
		return nil, errors.New("EMBEDDING_BASE_URL environment variable not set")
	}

	maxLength, err := parseMaxLength(maxLen)
	if err != nil {
		return nil, err
	}

	return &Config{
		Enabled:     enabled == "true",
		Source:      source,
		Model:       model,
		MaxLength:   maxLength,
		BaseURL:     baseUrl,
		ObjectStore: loadObjectStoreFromEnv(),
	}, nil
}

// parseMaxLength reads the width stored vectors are truncated to. Zero or a
// negative width would store empty vectors or panic mid-load, after the batch's
// articles are already written.
func parseMaxLength(raw string) (*int, error) {
	if raw == "" {
		return nil, nil
	}
	length, err := strconv.Atoi(raw)
	if err != nil || length <= 0 {
		return nil, fmt.Errorf("EMBEDDING_MAX_LENGTH must be a positive integer, got %q", raw)
	}
	return &length, nil
}

func loadObjectStoreFromEnv() ObjectStoreConfig {
	return ObjectStoreConfig{
		Endpoint:     os.Getenv("EMBEDDING_S3_ENDPOINT"),
		Region:       os.Getenv("EMBEDDING_S3_REGION"),
		Bucket:       os.Getenv("EMBEDDING_S3_BUCKET"),
		Key:          os.Getenv("EMBEDDING_S3_KEY"),
		AccessKey:    os.Getenv("EMBEDDING_S3_ACCESS_KEY"),
		SecretKey:    os.Getenv("EMBEDDING_S3_SECRET_KEY"),
		UsePathStyle: os.Getenv("EMBEDDING_S3_USE_PATH_STYLE") == "true",
		LocalPath:    os.Getenv("EMBEDDING_FILE_PATH"),
	}
}
