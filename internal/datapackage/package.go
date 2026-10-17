// Package datapackage writes and opens Frictionless Data Package v2
// descriptors whose resources are verified by sha256 and byte count on every
// read.
package datapackage

import "github.com/DjordjeVuckovic/tusker/pkg/apis/datamapping"

const (
	// Profile is the Data Package v2 profile every written descriptor declares.
	Profile        = "https://datapackage.org/profiles/2.0/datapackage.json"
	DescriptorName = "datapackage.json"
	ArticleSchema  = "article/v2"
)

// Resource names the canonical, embeddings and query-vectors packages use for
// their data file.
const (
	ArticlesResource     = "articles"
	EmbeddingsResource   = "embeddings"
	QueryVectorsResource = "query-vectors"
)

// Package is the descriptor: the Data Package v2 subset in use plus the
// custom properties shared by the canonical, embeddings and raw packages.
type Package struct {
	Schema        string      `json:"$schema"`
	Name          string      `json:"name"`
	Version       string      `json:"version,omitempty"`
	ArticleSchema string      `json:"articleSchema,omitempty"`
	Language      string      `json:"language,omitempty"`
	Mapping       *FileRef    `json:"mapping,omitempty"`
	IDStrategy    *IDStrategy `json:"idStrategy,omitempty"`
	ProducedBy    *ProducedBy `json:"producedBy,omitempty"`
	Report        *Report     `json:"report,omitempty"`
	Embedding     *Embedding  `json:"embedding,omitempty"`
	Canonical     *Ref        `json:"canonical,omitempty"`
	Suite         *Suite      `json:"suite,omitempty"`
	Sources       []Source    `json:"sources,omitempty"`
	Resources     []Resource  `json:"resources" schema:"required,minItems=1"`
}

// Resource is one file of the package. Path is relative to the descriptor;
// Hash is "sha256:<hex>".
type Resource struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Format string `json:"format"`
	Bytes  int64  `json:"bytes"`
	Hash   string `json:"hash"`
}

type Source struct {
	Title   string `json:"title,omitempty"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
}

type FileRef struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

// IDStrategy records how article ids were minted. Kind is the mapping's
// closed set, so an unknown kind fails Write and Open.
type IDStrategy struct {
	Kind      datamapping.IdStrategyKind `json:"kind"`
	Source    string                     `json:"source,omitempty"`
	Namespace string                     `json:"namespace,omitempty"`
}

type ProducedBy struct {
	Tool    string `json:"tool"`
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
}

// Report counts rows through the producing stage. Dropped is keyed by reason.
type Report struct {
	Read    int64            `json:"read"`
	Written int64            `json:"written"`
	Dropped map[string]int64 `json:"dropped,omitempty"`
	Deduped int64            `json:"deduped"`
}

// Embedding describes how the vectors of an embeddings package were made.
// MaxTokens caps the model input; Dim is the output width. BatchSize and DType
// shift output bytes slightly but are not version inputs. Limit is set only on
// a --limit trial, which loaders refuse unless asked to load one.
type Embedding struct {
	Model            string `json:"model"`
	HuggingFaceModel string `json:"hfModel"`
	Revision         string `json:"revision"`
	Tag              string `json:"tag"`
	Dim              int    `json:"dim"`
	Recipe           string `json:"recipe"`
	Pooling          string `json:"pooling"`
	Normalized       bool   `json:"normalized"`
	MaxTokens        int    `json:"maxTokens"`
	BatchSize        int    `json:"batchSize"`
	DType            string `json:"dtype"`
	Limit            *int   `json:"limit,omitempty"`
}

// Ref names another package and pins the hash of the resource it was built from.
type Ref struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	ResourceHash string `json:"resourceHash"`
}

// Suite pins the bench suite a query-vectors package was embedded from. Hash
// is the sha256 of suite.yaml as "sha256:<hex>".
type Suite struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}
