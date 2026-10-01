package testing

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestNewESContainer_CapsTheHeapAsComposeDoes(t *testing.T) {
	const composeHeapBytes = 512 << 20
	container := NewESContainer(context.Background(), t)

	res, err := http.Get(container.Address + "/_nodes/stats/jvm")
	if err != nil {
		t.Fatalf("node stats: %v", err)
	}
	defer res.Body.Close()

	var stats struct {
		Nodes map[string]struct {
			JVM struct {
				Mem struct {
					HeapMaxInBytes int64 `json:"heap_max_in_bytes"`
				} `json:"mem"`
			} `json:"jvm"`
		} `json:"nodes"`
	}
	if err := json.NewDecoder(res.Body).Decode(&stats); err != nil {
		t.Fatalf("decode node stats: %v", err)
	}
	if len(stats.Nodes) == 0 {
		t.Fatal("node stats listed no nodes")
	}
	for name, node := range stats.Nodes {
		if got := node.JVM.Mem.HeapMaxInBytes; got > composeHeapBytes {
			t.Errorf("node %s heap max = %d bytes, want at most %d", name, got, composeHeapBytes)
		}
	}
}
