package config

import (
	"strings"
	"testing"
)

func TestBlobCacheCap(t *testing.T) {
	token := strings.Repeat("j", 40)
	c, err := LoadWorker(env(map[string]string{"PODIUM_JUDGE_TOKEN": token}))
	if err != nil || c.BlobCacheMB != 2048 {
		t.Fatalf("default: %d %v", c.BlobCacheMB, err)
	}
	c, err = LoadWorker(env(map[string]string{"PODIUM_JUDGE_TOKEN": token, "PODIUM_JUDGE_BLOB_CACHE_MB": "512"}))
	if err != nil || c.BlobCacheMB != 512 {
		t.Errorf("512: %d %v", c.BlobCacheMB, err)
	}
	for _, bad := range []string{"10", "lots", "-1"} {
		_, err := LoadWorker(env(map[string]string{"PODIUM_JUDGE_TOKEN": token, "PODIUM_JUDGE_BLOB_CACHE_MB": bad}))
		if err == nil || !strings.Contains(err.Error(), "PODIUM_JUDGE_BLOB_CACHE_MB must be a whole number from 64") {
			t.Errorf("%q accepted or unclear: %v", bad, err)
		}
	}
}
