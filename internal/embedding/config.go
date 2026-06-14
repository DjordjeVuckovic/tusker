package embedding

import (
	"errors"
	"os"
	"strconv"
)

// Source selects how document embeddings are produced.
type Source string

const (
	// SourceOnline generates embeddings inline during ingestion via Ollama.
	SourceOnline Source = "online"
	// SourceFile loads precomputed embeddings from an object store (ingest embeddings).
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

	return &Config{
		Enabled: enabled == "true",
		Source:  source,
		Model:   model,
		MaxLength: func() *int {
			if maxLen == "" {
				return nil
			}
			val, err := strconv.Atoi(maxLen)
			if err != nil {
				return nil
			}
			return &val
		}(),
		BaseURL:     baseUrl,
		ObjectStore: loadObjectStoreFromEnv(),
	}, nil
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
