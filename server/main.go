// Command picode-docker-server is picode-docker's background program
// (PiCode ADR-0230: process, pages, commands). PiCode starts it while the
// extension is on in a workspace and is the only caller: every request
// carries X-PiCode-Proxy-Secret, the value PiCode gave this process in
// PICODE_EXT_PROXY_SECRET.
//
//	GET  /containers            every container, grouped by compose project
//	GET  /containers/{id}       one container: detail, a resource sample, logs
//	POST /containers/{id}/action {action: start|stop|restart}
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/cfpperche/picode-docker/engine"
)

var secret = os.Getenv("PICODE_EXT_PROXY_SECRET")

type group struct {
	Project    string      `json:"project"`
	Containers []container `json:"containers"`
}

type container struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	State   string   `json:"state"`
	Status  string   `json:"status"`
	Service string   `json:"service,omitempty"`
	Health  string   `json:"health,omitempty"`
	Actions []string `json:"actions"`
}

func toRow(c engine.Container) container {
	var actions []string
	for _, verb := range []string{"start", "stop", "restart"} {
		if validAction(verb, c.State) {
			actions = append(actions, verb)
		}
	}
	return container{ID: c.ID, Name: c.Name, Image: c.Image, State: c.State, Status: c.Status, Service: c.Service, Health: c.Health, Actions: actions}
}

// actionPast is the participle for each valid action's messages ("stop" is
// not "stoped", %sed is not English).
var actionPast = map[string]string{"start": "started", "stop": "stopped", "restart": "restarted"}

func validAction(action, state string) bool {
	switch action {
	case "start":
		return state == "created" || state == "exited"
	case "stop":
		return state == "running" || state == "restarting"
	case "restart":
		return state == "running"
	}
	return false
}

func connect(ctx context.Context) (*engine.Client, error) {
	c, err := engine.LocalClient(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Check(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func listContainers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	c, err := connect(ctx)
	if err != nil {
		reply(w, 502, map[string]string{"message": err.Error()})
		return
	}
	defer c.Close()
	rows, err := c.Containers(ctx)
	if err != nil {
		reply(w, 502, map[string]string{"message": err.Error()})
		return
	}
	byProject := map[string]*group{}
	for _, row := range rows {
		g := byProject[row.Project]
		if g == nil {
			g = &group{Project: row.Project}
			byProject[row.Project] = g
		}
		g.Containers = append(g.Containers, toRow(row))
	}
	names := make([]string, 0, len(byProject))
	for name := range byProject {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == "") != (names[j] == "") {
			return names[j] == ""
		}
		return names[i] < names[j]
	})
	groups := make([]group, 0, len(names))
	for _, name := range names {
		groups = append(groups, *byProject[name])
	}
	reply(w, 200, map[string]any{"groups": groups, "count": len(rows)})
}

type detail struct {
	container
	StartedAt     string   `json:"startedAt,omitempty"`
	RestartCount  int      `json:"restartCount"`
	ExitCode      int      `json:"exitCode"`
	OOMKilled     bool     `json:"oomKilled"`
	CPUPercent    *float64 `json:"cpuPercent,omitempty"`
	MemoryBytes   uint64   `json:"memoryBytes,omitempty"`
	LimitBytes    uint64   `json:"limitBytes,omitempty"`
	StatsError    string   `json:"statsError,omitempty"`
	Logs          string   `json:"logs,omitempty"`
	LogsTruncated bool     `json:"logsTruncated,omitempty"`
	LogsError     string   `json:"logsError,omitempty"`
}

func containerDetail(w http.ResponseWriter, r *http.Request, id string) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, err := connect(ctx)
	if err != nil {
		reply(w, 502, map[string]string{"message": err.Error()})
		return
	}
	defer c.Close()
	full, err := c.Inspect(ctx, id)
	if err != nil {
		var apiErr *engine.APIError
		code := 502
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			code = 404
		}
		reply(w, code, map[string]string{"message": err.Error()})
		return
	}
	d := detail{container: toRow(full), StartedAt: full.StartedAt, RestartCount: full.RestartCount, ExitCode: full.ExitCode, OOMKilled: full.OOMKilled}
	if s, err := c.Stats(ctx, id); err == nil {
		d.CPUPercent, d.MemoryBytes, d.LimitBytes = &s.CPUPercent, s.MemoryBytes, s.LimitBytes
	} else {
		d.StatsError = err.Error()
	}
	if logs, err := c.Logs(ctx, full); err == nil {
		d.Logs, d.LogsTruncated = logs.Text, logs.Truncated
	} else {
		d.LogsError = err.Error()
	}
	reply(w, 200, d)
}

func containerAction(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		reply(w, 400, map[string]string{"message": "give an action"})
		return
	}
	past, ok := actionPast[req.Action]
	if !ok {
		reply(w, 400, map[string]string{"message": "action must be start, stop or restart"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, err := connect(ctx)
	if err != nil {
		reply(w, 502, map[string]string{"message": err.Error()})
		return
	}
	defer c.Close()
	full, err := c.Inspect(ctx, id)
	if err != nil {
		reply(w, 502, map[string]string{"message": err.Error()})
		return
	}
	if !validAction(req.Action, full.State) {
		reply(w, 409, map[string]string{"message": fmt.Sprintf("%s cannot be %s from its current state (%s)", full.Name, past, full.State)})
		return
	}
	if err := c.Mutate(ctx, id, req.Action); err != nil {
		reply(w, 502, map[string]string{"message": err.Error()})
		return
	}
	reply(w, 200, map[string]string{"message": strings.ToUpper(past[:1]) + past[1:] + " " + full.Name + "."})
}

func reply(w http.ResponseWriter, code int, body any) {
	data, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(data)
}

func withAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if secret == "" || r.Header.Get("X-PiCode-Proxy-Secret") != secret {
			reply(w, 403, map[string]string{"message": "only PiCode may call this program"})
			return
		}
		fn(w, r)
	}
}

func main() {
	port := os.Getenv("PICODE_EXT_PORT")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /containers", withAuth(listContainers))
	mux.HandleFunc("GET /containers/{id}", withAuth(func(w http.ResponseWriter, r *http.Request) { containerDetail(w, r, r.PathValue("id")) }))
	mux.HandleFunc("POST /containers/{id}/action", withAuth(func(w http.ResponseWriter, r *http.Request) { containerAction(w, r, r.PathValue("id")) }))
	log.Printf("picode-docker server on 127.0.0.1:%s", port)
	if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
		log.Fatal(err)
	}
}
