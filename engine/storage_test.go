package engine

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

const dfJSON = `{"Images":[{"Id":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","RepoDigests":["redis@sha256:x"],"Size":1000,"SharedSize":200,"Containers":0},{"Id":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","RepoDigests":["pg@sha256:y"],"Size":500,"SharedSize":0,"Containers":2},{"Id":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","RepoTags":["my-bot:latest"],"Size":400,"SharedSize":0,"Containers":0}],
"BuildCache":[{"Size":300,"InUse":false},{"Size":50,"InUse":true}],
"Volumes":[{"Name":"free_db","Labels":{"com.docker.compose.project":"shop"},"UsageData":{"Size":700,"RefCount":0}},{"Name":"used_db","UsageData":{"Size":900,"RefCount":1}},{"Name":"unknown","UsageData":{"Size":-1,"RefCount":0}}]}`

// What Docker keeps reads as PiCode's rows: unused images without their
// shared layers, the build cache not in use, each volume no container uses
// offered unchecked, and what containers use shown as data.
func TestStorageItems(t *testing.T) {
	c := socketClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/system/df") {
			w.Write([]byte(dfJSON))
			return
		}
		http.NotFound(w, r)
	})
	items, err := c.StorageItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]StorageItem{}
	for _, it := range items {
		got[it.ID] = it
	}
	if a := got["image:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]; a.Reclaimable != 800 || !a.Unchecked {
		t.Fatalf("a downloaded unused image = %+v", a)
	}
	if got["images-used"].Kind != "data" || got["images-used"].Reclaimable != 0 {
		t.Fatalf("images in use = %+v", got["images-used"])
	}
	for _, it := range items {
		if it.Reclaimable > 0 && !it.Unchecked && it.ID != "build-cache" {
			t.Fatalf("offered checked: %+v", it)
		}
	}
	if l := got["image:sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"]; l.Reclaimable != 400 || !l.Unchecked || l.Title != "Image my-bot:latest" {
		t.Fatalf("an image built here = %+v", l)
	}
	if !strings.Contains(got["volume:free_db"].Detail, "project shop") {
		t.Fatalf("volume detail = %q", got["volume:free_db"].Detail)
	}
	if got["build-cache"].Reclaimable != 300 {
		t.Fatalf("cache = %+v", got["build-cache"])
	}
	if v := got["volume:free_db"]; v.Reclaimable != 700 || !v.Unchecked {
		t.Fatalf("free volume = %+v", v)
	}
	if _, ok := got["volume:used_db"]; ok {
		t.Fatal("a volume in use was offered")
	}
	if _, ok := got["volume:unknown"]; ok {
		t.Fatal("a volume Docker could not measure was offered")
	}
}

// A volume is checked again before it goes: in use or gone is ErrNotFree;
// prunes report what Docker freed.
func TestCleanStorageItem(t *testing.T) {
	var deleted, images []string
	c := socketClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/system/df"):
			w.Write([]byte(dfJSON))
		case r.Method == "DELETE" && strings.Contains(r.URL.Path, "/volumes/"):
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(204)
		case r.Method == "DELETE" && strings.Contains(r.URL.Path, "/images/"):
			if r.URL.Query().Get("force") != "" {
				t.Errorf("forced image removal: %s", r.URL)
			}
			images = append(images, r.URL.Path)
			w.Write([]byte(`[]`))
		case strings.Contains(r.URL.Path, "/images/prune"):
			t.Error("an image prune: it would take images built here")
		case strings.HasSuffix(r.URL.Path, "/build/prune"):
			if r.URL.Query().Get("all") != "true" {
				t.Errorf("build prune without all: %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"SpaceReclaimed":300}`))
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	if n, err := c.CleanStorageItem(ctx, "image:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil || n != 800 || len(images) != 1 || !strings.Contains(images[0], "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatalf("a downloaded image = %d %v %v", n, err, images)
	}
	if _, err := c.CleanStorageItem(ctx, "images-unused"); err == nil {
		t.Fatal("a sweep of every image is still accepted")
	}
	if n, err := c.CleanStorageItem(ctx, "image:sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"); err != nil || n != 400 || len(images) != 2 {
		t.Fatalf("an image built here = %d %v %v", n, err, images)
	}
	if _, err := c.CleanStorageItem(ctx, "image:sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); !errors.Is(err, ErrNotFree) || len(images) != 2 {
		t.Fatalf("an image in use: %v %v", err, images)
	}
	if _, err := c.CleanStorageItem(ctx, "image:sha256:../x"); err == nil {
		t.Fatal("an invalid image id was accepted")
	}
	if n, err := c.CleanStorageItem(ctx, "build-cache"); err != nil || n != 300 {
		t.Fatalf("cache = %d %v", n, err)
	}
	if n, err := c.CleanStorageItem(ctx, "volume:free_db"); err != nil || n != 700 || len(deleted) != 1 {
		t.Fatalf("free volume = %d %v %v", n, err, deleted)
	}
	if _, err := c.CleanStorageItem(ctx, "volume:used_db"); !errors.Is(err, ErrNotFree) || len(deleted) != 1 {
		t.Fatalf("a volume in use: %v, deleted %v", err, deleted)
	}
	if _, err := c.CleanStorageItem(ctx, "volume:gone"); !errors.Is(err, ErrNotFree) {
		t.Fatalf("a volume gone: %v", err)
	}
	if _, err := c.CleanStorageItem(ctx, "volume:../etc"); err == nil {
		t.Fatal("an invalid name was accepted")
	}
	if _, err := c.CleanStorageItem(ctx, "everything"); err == nil {
		t.Fatal("an unknown item was accepted")
	}
}
