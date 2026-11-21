package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHypothesisCacheEvictsBySizeAndUse(t *testing.T) {
	dir := t.TempDir()
	snapshot := hypothesisSnapshot{
		Head: strings.Repeat("a", 40), MergeBase: strings.Repeat("b", 40),
		Hypotheses: []Hypothesis{{Slug: "empty-input", Question: "Does empty input fail?"}},
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var caches []hypothesisCache
	for _, branch := range []string{"first", "second", "third", "large"} {
		caches = append(caches, hypothesisCache{dir: dir, key: hypothesisDigest(branch), maxBytes: 2 * int64(len(data))})
	}
	for i, cache := range caches[:2] {
		saveHypothesisSnapshot(t, cache, "", snapshot)
		older := time.Unix(int64(i)+1, 0)
		err = os.Chtimes(filepath.Join(dir, cache.fileName()), older, older)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, firstStamp := readHypothesisSnapshot(t, caches[0])
	saveHypothesisSnapshot(t, caches[2], "", snapshot)
	if _, stamp := readHypothesisSnapshot(t, caches[1]); stamp != "" {
		t.Fatal("least recently used snapshot remains in the cache")
	}
	if _, stamp := readHypothesisSnapshot(t, caches[0]); stamp != firstStamp {
		t.Fatal("recently read snapshot was evicted")
	}

	oversize := snapshot
	oversize.Hypotheses = []Hypothesis{{Question: strings.Repeat("x", len(data)*2)}}
	saveHypothesisSnapshot(t, caches[3], "", oversize)
	if _, stamp := readHypothesisSnapshot(t, caches[3]); stamp != "" {
		t.Fatal("oversized snapshot entered the cache")
	}
	if _, stamp := readHypothesisSnapshot(t, caches[2]); stamp == "" {
		t.Fatal("oversized snapshot evicted an existing entry")
	}
}

func TestHypothesisCacheRejectsStaleWrite(t *testing.T) {
	cache := hypothesisCache{dir: t.TempDir(), key: hypothesisDigest("branch"), maxBytes: hypothesisCacheBytes}
	first := hypothesisSnapshot{Head: strings.Repeat("a", 40), MergeBase: strings.Repeat("b", 40)}
	saveHypothesisSnapshot(t, cache, "", first)
	_, stamp := readHypothesisSnapshot(t, cache)
	second := first
	second.Head = strings.Repeat("c", 40)
	saveHypothesisSnapshot(t, cache, stamp, second)
	saveHypothesisSnapshot(t, cache, stamp, first)
	if got, _ := readHypothesisSnapshot(t, cache); got.Head != second.Head {
		t.Errorf("cached commit = %s, want %s", got.Head, second.Head)
	}
}

func readHypothesisSnapshot(t *testing.T, cache hypothesisCache) (hypothesisSnapshot, string) {
	t.Helper()
	snapshot, stamp, err := cache.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, stamp
}

func saveHypothesisSnapshot(t *testing.T, cache hypothesisCache, stamp string, snapshot hypothesisSnapshot) {
	t.Helper()
	err := cache.save(t.Context(), stamp, snapshot)
	if err != nil {
		t.Fatal(err)
	}
}
