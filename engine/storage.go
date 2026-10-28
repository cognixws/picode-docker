package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// What Docker keeps on the computer, for PiCode's Settings ▸ Storage (the
// storageProvider door, PiCode ADR-0377 H5). Measured from one
// GET /system/df; given back by Docker's own commands, one reviewed item
// at a time: unused images (prune), the build cache (prune), one volume no
// container uses (remove, checked again first).

// StorageItem is one row PiCode shows.
type StorageItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Detail      string `json:"detail,omitempty"`
	Bytes       int64  `json:"bytes"`
	Reclaimable int64  `json:"reclaimable"`
	Kind        string `json:"kind"`
	// Unchecked: PiCode offers it unchecked (a volume is data: picked one
	// by one, never in a sweep).
	Unchecked bool `json:"unchecked,omitempty"`
}

type diskUsage struct {
	Images []struct {
		ID          string   `json:"Id"`
		RepoTags    []string `json:"RepoTags"`
		RepoDigests []string `json:"RepoDigests"`
		Size        int64    `json:"Size"`
		SharedSize  int64    `json:"SharedSize"`
		Containers  int64    `json:"Containers"`
	} `json:"Images"`
	BuildCache []struct {
		Size  int64 `json:"Size"`
		InUse bool  `json:"InUse"`
	} `json:"BuildCache"`
	Volumes []struct {
		Name      string            `json:"Name"`
		Labels    map[string]string `json:"Labels"`
		UsageData struct {
			Size     int64 `json:"Size"`
			RefCount int64 `json:"RefCount"`
		} `json:"UsageData"`
	} `json:"Volumes"`
}

// maxVolumes is how many unused volumes are listed, largest first.
const maxVolumes = 50

func (c *Client) diskUsage(ctx context.Context) (diskUsage, error) {
	var du diskUsage
	err := c.get(ctx, "/v"+apiVersion+"/system/df", &du)
	return du, err
}

// ownSize is what removing an image gives back: its shared layers stay.
func ownSize(size, shared int64) int64 {
	if shared > 0 && shared <= size {
		return size - shared
	}
	return size
}

// StorageItems measures what Docker keeps. Each image no container uses is
// a row of its own, offered unchecked: Docker cannot tell one downloaded
// from one built here (with the containerd store both carry a digest), and
// one built here cannot be downloaded again. So is each volume no
// container uses, with the compose or Supabase project it belongs to when
// its labels say so. Only the build cache is offered checked.
func (c *Client) StorageItems(ctx context.Context) ([]StorageItem, error) {
	du, err := c.diskUsage(ctx)
	if err != nil {
		return nil, err
	}
	var used int64
	type local struct {
		id, name string
		size     int64
	}
	var locals []local
	for _, im := range du.Images {
		own := ownSize(im.Size, im.SharedSize)
		switch {
		case im.Containers != 0:
			used += im.Size
		default:
			name := shortID(im.ID)
			for _, tag := range im.RepoTags {
				if tag != "<none>:<none>" {
					name = tag
					break
				}
			}
			locals = append(locals, local{im.ID, name, own})
		}
	}
	var cache int64
	for _, b := range du.BuildCache {
		if !b.InUse {
			cache += b.Size
		}
	}
	var out []StorageItem
	sort.Slice(locals, func(i, j int) bool { return locals[i].size > locals[j].size })
	for i, l := range locals {
		if i >= maxVolumes || l.size <= 0 {
			break
		}
		out = append(out, StorageItem{ID: "image:" + l.id, Title: "Image " + l.name, Bytes: l.size, Reclaimable: l.size, Kind: "leftover", Unchecked: true,
			Detail: "No container uses it. One downloaded comes back with a pull; one built here only with a new build."})
	}
	if used > 0 {
		out = append(out, StorageItem{ID: "images-used", Title: "Images your containers use", Bytes: used, Kind: "data"})
	}
	if cache > 0 {
		out = append(out, StorageItem{ID: "build-cache", Title: "Build cache", Bytes: cache, Reclaimable: cache, Kind: "safe",
			Detail: "Layers kept to build faster; a build makes them again."})
	}
	type vol struct {
		name, project string
		size          int64
	}
	var free []vol
	var usedVol int64
	for _, v := range du.Volumes {
		if v.UsageData.Size < 0 {
			continue // Docker could not measure it
		}
		if v.UsageData.RefCount == 0 {
			project := v.Labels["com.docker.compose.project"]
			if project == "" {
				project = v.Labels["com.supabase.cli.project"]
			}
			free = append(free, vol{v.Name, project, v.UsageData.Size})
		} else {
			usedVol += v.UsageData.Size
		}
	}
	sort.Slice(free, func(i, j int) bool { return free[i].size > free[j].size })
	for i, v := range free {
		if i >= maxVolumes || v.size <= 0 {
			break
		}
		detail := "No container uses it. What it holds is lost when it goes."
		if v.project != "" {
			detail = "Belongs to project " + v.project + ", kept while it is down. What it holds is lost when it goes."
		}
		out = append(out, StorageItem{ID: "volume:" + v.name, Title: "Volume " + v.name, Bytes: v.size, Reclaimable: v.size, Kind: "leftover", Unchecked: true, Detail: detail})
	}
	if usedVol > 0 {
		out = append(out, StorageItem{ID: "volumes-used", Title: "Volumes your containers use", Bytes: usedVol, Kind: "data"})
	}
	return out, nil
}

func shortID(id string) string {
	if i := strings.IndexByte(id, ':'); i >= 0 {
		id = id[i+1:]
	}
	if len(id) > 12 {
		id = id[:12]
	}
	return id
}

// ErrNotFree: the item is no longer there or something uses it now.
var ErrNotFree = errors.New("no longer free")

var volumeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,254}$`)

// CleanStorageItem gives one item back and answers the bytes Docker freed.
func (c *Client) CleanStorageItem(ctx context.Context, id string) (int64, error) {
	switch {
	case strings.HasPrefix(id, "image:"):
		imageID := id[len("image:"):]
		du, err := c.diskUsage(ctx)
		if err != nil {
			return 0, err
		}
		for _, im := range du.Images {
			if im.ID != imageID {
				continue
			}
			if im.Containers != 0 {
				return 0, ErrNotFree
			}
			if err := c.removeImage(ctx, im.ID); err != nil {
				return 0, err
			}
			return ownSize(im.Size, im.SharedSize), nil
		}
		return 0, ErrNotFree
	case id == "build-cache":
		// All the cache not in use, as measured (`builder prune -a`).
		return c.prune(ctx, "/v"+apiVersion+"/build/prune?all=true")
	case len(id) > 7 && id[:7] == "volume:":
		name := id[7:]
		if !volumeName.MatchString(name) {
			return 0, errors.New("invalid volume name")
		}
		du, err := c.diskUsage(ctx)
		if err != nil {
			return 0, err
		}
		for _, v := range du.Volumes {
			if v.Name != name {
				continue
			}
			if v.UsageData.RefCount != 0 {
				return 0, ErrNotFree
			}
			res, err := c.request(ctx, "DELETE", "/v"+apiVersion+"/volumes/"+url.PathEscape(name))
			if err != nil {
				var api *APIError
				if errors.As(err, &api) && (api.Status == 404 || api.Status == 409) {
					return 0, ErrNotFree // gone, or a container took it since
				}
				return 0, err
			}
			_ = res.Body.Close()
			return max(v.UsageData.Size, 0), nil
		}
		return 0, ErrNotFree
	}
	return 0, fmt.Errorf("unknown item %q", id)
}

var imageID = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// removeImage removes one image by id, never forced: Docker refuses one a
// container still uses (409).
func (c *Client) removeImage(ctx context.Context, id string) error {
	if !imageID.MatchString(id) {
		return errors.New("invalid image id")
	}
	res, err := c.request(ctx, "DELETE", "/v"+apiVersion+"/images/"+url.PathEscape(id))
	if err != nil {
		var api *APIError
		if errors.As(err, &api) && (api.Status == 404 || api.Status == 409) {
			return ErrNotFree
		}
		return err
	}
	_ = res.Body.Close()
	return nil
}

func (c *Client) prune(ctx context.Context, path string) (int64, error) {
	res, err := c.request(ctx, "POST", path)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	var out struct {
		SpaceReclaimed int64 `json:"SpaceReclaimed"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 4*1024*1024)).Decode(&out)
	return max(out.SpaceReclaimed, 0), nil
}
