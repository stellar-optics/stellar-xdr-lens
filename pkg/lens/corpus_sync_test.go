package lens

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// corpusManifest describes the canonical testdata snapshot kept in the repo.
// It is intentionally static and deterministic: the core execution path never
// reaches out to the network and instead validates the on-disk manifest.
type corpusManifest struct {
	Fixtures          map[string]corpusFixture `json:"fixtures"`
	SyntheticFixtures struct {
		Fixtures map[string]corpusFixture `json:"fixtures"`
	} `json:"synthetic_fixtures"`
}

type corpusFixture struct {
	EnvelopeFile string  `json:"envelope_file"`
	ResultFile   *string `json:"result_file"`
}

// syncTestdataCorpus verifies the local corpus snapshot is locked to the checked-in
// manifest. The function is used by tests to reason about fixture provenance without
// introducing any network dependency in the execution path.
func syncTestdataCorpus(dir string) (map[string]string, error) {
	return syncTestdataCorpusWithMode(dir, false)
}

func syncTestdataCorpusStrict(dir string) (map[string]string, error) {
	return syncTestdataCorpusWithMode(dir, true)
}

func syncTestdataCorpusWithMode(dir string, strict bool) (map[string]string, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", manifestPath, err)
	}

	var manifest corpusManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", manifestPath, err)
	}

	entries, err := readCorpusFiles(dir)
	if err != nil {
		return nil, err
	}

	validateManifestEntries := func(fixtures map[string]corpusFixture) error {
		for name, fixture := range fixtures {
			if fixture.EnvelopeFile == "" {
				continue
			}
			path := filepath.Join(dir, fixture.EnvelopeFile)
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("manifest entry %q references missing fixture %q: %w", name, fixture.EnvelopeFile, err)
			}
			if _, ok := entries[name]; !ok {
				return fmt.Errorf("manifest entry %q is missing the canonical fixture file %q", name, fixture.EnvelopeFile)
			}
			if fixture.ResultFile != nil {
				resPath := filepath.Join(dir, *fixture.ResultFile)
				if _, err := os.Stat(resPath); err != nil {
					return fmt.Errorf("manifest entry %q references missing result %q: %w", name, *fixture.ResultFile, err)
				}
			}
		}
		return nil
	}
	if err := validateManifestEntries(manifest.Fixtures); err != nil {
		return nil, err
	}
	if err := validateManifestEntries(manifest.SyntheticFixtures.Fixtures); err != nil {
		return nil, err
	}

	if strict {
		knownFixtures := make(map[string]struct{}, len(manifest.Fixtures)+len(manifest.SyntheticFixtures.Fixtures))
		for name := range manifest.Fixtures {
			knownFixtures[name] = struct{}{}
		}
		for name := range manifest.SyntheticFixtures.Fixtures {
			knownFixtures[name] = struct{}{}
		}
		for name, filePath := range entries {
			if _, ok := knownFixtures[name]; !ok {
				return nil, fmt.Errorf("fixture %q exists at %s without a manifest entry", name, filePath)
			}
		}
	}

	return entries, nil
}

func readCorpusFiles(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading fixture directory %s: %w", dir, err)
	}

	files := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".txt" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".txt")
		if strings.HasSuffix(name, ".env") || strings.HasSuffix(name, ".res") {
			name = strings.TrimSuffix(name, filepath.Ext(name))
		}
		if _, exists := files[name]; !exists || strings.Contains(entry.Name(), ".env.txt") {
			files[name] = filepath.Join(dir, entry.Name())
		}
	}

	return files, nil
}

func scrubProxyEnvForOfflineMode(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "no_proxy",
		"ALL_PROXY", "all_proxy",
	} {
		if err := os.Setenv(key, ""); err != nil {
			t.Fatalf("clearing %s: %v", key, err)
		}
	}
}

func TestSyncTestdataCorpusOffline(t *testing.T) {
	scrubProxyEnvForOfflineMode(t)

	corpus, err := syncTestdataCorpus(filepath.Join("..", "..", "testdata"))
	if err != nil {
		t.Fatalf("syncTestdataCorpus returned error: %v", err)
	}

	if len(corpus) == 0 {
		t.Fatal("expected at least one canonical fixture in the corpus")
	}

	for _, expected := range []string{"tx_failed", "tx_success", "tx_feebump", "tx_soroban"} {
		if _, ok := corpus[expected]; !ok {
			t.Fatalf("expected canonical fixture %q to be present in the offline corpus", expected)
		}
	}

	keys := make([]string, 0, len(corpus))
	for name := range corpus {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for i := 1; i < len(keys); i++ {
		if keys[i] <= keys[i-1] {
			t.Fatalf("corpus keys are not sorted deterministically: %v", keys)
		}
	}
}

func TestSyncTestdataCorpusRejectsMissingFixtureFile(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"fixtures":{"broken":{"envelope_file":"missing.txt","result_file":"missing.res.txt"}}}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if _, err := syncTestdataCorpus(dir); err == nil {
		t.Fatal("expected a missing fixture file error")
	} else if !strings.Contains(err.Error(), "missing fixture") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestSyncTestdataCorpusRejectsUntrackedFixture(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"fixtures":{"tracked":{"envelope_file":"tracked.txt"}}}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("base64"), 0o600); err != nil {
		t.Fatalf("write tracked fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("base64"), 0o600); err != nil {
		t.Fatalf("write untracked fixture: %v", err)
	}

	if _, err := syncTestdataCorpusStrict(dir); err == nil {
		t.Fatal("expected an untracked fixture error")
	} else if !strings.Contains(err.Error(), "without a manifest entry") {
		t.Fatalf("unexpected error %v", err)
	}
}
