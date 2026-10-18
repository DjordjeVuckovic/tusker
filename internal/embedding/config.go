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
	Enabled bool
	Source  Source
	// Model is the Ollama model name, without the @dim tag.
	Model       string
	Dim         int
	BaseURL     string
	ObjectStore ObjectStoreConfig
}

func LoadConfigFromEnv() (*Config, error) {
	enabled := os.Getenv("EMBEDDING_ENABLED")
	model := os.Getenv("EMBEDDING_MODEL")
	baseUrl := os.Getenv("EMBEDDING_BASE_URL")

	source := Source(os.Getenv("EMBEDDING_SOURCE"))
	if source == "" {
		source = SourceOnline
	}

	// The Ollama base URL is only needed for online generation.
	if source == SourceOnline && enabled == "true" && baseUrl == "" {
		return nil, errors.New("EMBEDDING_BASE_URL environment variable not set")
	}

	// Ignoring the old name would quietly store vectors at DefaultDim.
	if _, set := os.LookupEnv("EMBEDDING_MAX_LENGTH"); set {
		return nil, errors.New("EMBEDDING_MAX_LENGTH is renamed EMBEDDING_DIM")
	}
	dim, err := DimFromEnv()
	if err != nil {
		return nil, err
	}

	return &Config{
		Enabled:     enabled == "true",
		Source:      source,
		Model:       model,
		Dim:         dim,
		BaseURL:     baseUrl,
		ObjectStore: loadObjectStoreFromEnv(),
	}, nil
}

// DimFromEnv reads EMBEDDING_DIM, the width vectors are sliced to and tagged with.
func DimFromEnv() (int, error) {
	return parseDim(os.Getenv("EMBEDDING_DIM"))
}

func parseDim(raw string) (int, error) {
	if raw == "" {
		return DefaultDim, nil
	}
	dim, err := strconv.Atoi(raw)
	if err != nil || dim <= 0 {
		return 0, fmt.Errorf("EMBEDDING_DIM must be a positive integer, got %q", raw)
	}
	return dim, nil
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
