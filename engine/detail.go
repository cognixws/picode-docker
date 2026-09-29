package engine

import "context"

// Row is a container summarized for a list: id, name, image, state, the
// actions it can currently take from that state. The shape both the HTTP
// page server and the MCP server answer with.
type Row struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	State   string   `json:"state"`
	Status  string   `json:"status"`
	Project string   `json:"project,omitempty"`
	Service string   `json:"service,omitempty"`
	Health  string   `json:"health,omitempty"`
	Actions []string `json:"actions"`
}

// RowOf summarizes a Container as a Row, with the actions valid from its
// current state.
func RowOf(c Container) Row {
	var actions []string
	for _, verb := range []string{"start", "stop", "restart"} {
		if ValidAction(verb, c.State) {
			actions = append(actions, verb)
		}
	}
	return Row{ID: c.ID, Name: c.Name, Image: c.Image, State: c.State, Status: c.Status, Project: c.Project, Service: c.Service, Health: c.Health, Actions: actions}
}

// ActionPast is the participle for each valid action's messages ("stop" is
// not "stoped", %sed is not English).
var ActionPast = map[string]string{"start": "started", "stop": "stopped", "restart": "restarted"}

// ValidAction reports whether action makes sense from a container's
// current state.
func ValidAction(action, state string) bool {
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

// Connect dials the local Docker Engine and checks its API version.
func Connect(ctx context.Context) (*Client, error) {
	c, err := LocalClient(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Check(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Detail is one container's state, a resource sample and recent logs —
// everything the containers page and docker_container answer with.
type Detail struct {
	Row
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

// BuildDetail inspects a container, samples its resource use and reads its
// recent logs. Inspect failing is fatal (there is no container to describe);
// a stats or logs failure is reported in its own field instead, so one
// unreadable sample never hides the other two.
func BuildDetail(ctx context.Context, c *Client, id string) (Detail, error) {
	full, err := c.Inspect(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	d := Detail{Row: RowOf(full), StartedAt: full.StartedAt, RestartCount: full.RestartCount, ExitCode: full.ExitCode, OOMKilled: full.OOMKilled}
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
	return d, nil
}
