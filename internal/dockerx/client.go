package dockerx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"

	"github.com/guilhermepantoja789/docker-tui/internal/model"
)

// Client wraps the Docker Engine API with helpers suited for telemetry.
type Client struct {
	api     *client.Client
	host    model.Host
	closer  io.Closer
}

// Options configures NewClient.
type Options struct {
	Host    string
	Context string
}

// DockerContext is a named Docker CLI context.
type DockerContext struct {
	Name      string
	Current   bool
	Endpoints map[string]string
}

// NewClient creates a Docker API client from env, host, and/or context.
func NewClient(opts Options) (*Client, error) {
	hostName, hostAddr, err := resolveEndpoint(opts)
	if err != nil {
		return nil, err
	}

	clientOpts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}
	if hostAddr != "" {
		clientOpts = append(clientOpts, client.WithHost(hostAddr))
	}

	api, err := client.NewClientWithOpts(clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	display := hostName
	if display == "" {
		display = "default"
	}
	if hostAddr == "" {
		hostAddr = api.DaemonHost()
	}

	return &Client{
		api: api,
		host: model.Host{
			Name:    display,
			Address: hostAddr,
			Context: opts.Context,
		},
		closer: api,
	}, nil
}

func resolveEndpoint(opts Options) (name, addr string, err error) {
	if opts.Host != "" {
		return opts.Host, opts.Host, nil
	}
	if opts.Context == "" {
		current, err := CurrentContextName()
		if err != nil {
			return "", "", err
		}
		opts.Context = current
	}
	if opts.Context == "" || opts.Context == "default" {
		return "default", "", nil
	}

	ctxs, err := ListContexts()
	if err != nil {
		return "", "", err
	}
	for _, c := range ctxs {
		if c.Name != opts.Context {
			continue
		}
		ep := c.Endpoints["docker"]
		return c.Name, ep, nil
	}
	return "", "", fmt.Errorf("docker context %q not found", opts.Context)
}

// Host returns the connected host metadata.
func (c *Client) Host() model.Host {
	return c.host
}

// Close releases the underlying API client.
func (c *Client) Close() error {
	if c.closer == nil {
		return nil
	}
	return c.closer.Close()
}

// Ping checks daemon connectivity.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.api.Ping(ctx)
	if err != nil {
		return fmt.Errorf("ping docker: %w", err)
	}
	return nil
}

// ListContainers returns all containers (running and stopped).
func (c *Client) ListContainers(ctx context.Context) ([]model.Container, error) {
	items, err := c.api.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	out := make([]model.Container, 0, len(items))
	hostName := c.host.Name
	for _, it := range items {
		name := trimContainerName(it.Names)
		labels := it.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		out = append(out, model.Container{
			ID:      it.ID,
			Name:    name,
			Image:   it.Image,
			State:   it.State,
			Status:  it.Status,
			Labels:  copyStringMap(labels),
			Host:    hostName,
			Created: time.Unix(it.Created, 0).UTC(),
		})
	}
	return out, nil
}

// Events streams Docker events until ctx is cancelled.
func (c *Client) Events(ctx context.Context) (<-chan events.Message, <-chan error) {
	f := filters.NewArgs()
	f.Add("type", "container")
	f.Add("type", "network")
	f.Add("type", "volume")
	f.Add("type", "image")
	return c.api.Events(ctx, events.ListOptions{Filters: f})
}

// StatsOneShot fetches a single stats sample without streaming.
func (c *Client) StatsOneShot(ctx context.Context, containerID string) (*container.StatsResponse, error) {
	resp, err := c.api.ContainerStatsOneShot(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("stats %s: %w", shortID(containerID), err)
	}
	defer resp.Body.Close()

	var stats container.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("decode stats %s: %w", shortID(containerID), err)
	}
	return &stats, nil
}

// StartContainer starts a stopped container.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	if err := c.api.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return fmt.Errorf("start %s: %w", shortID(id), err)
	}
	return nil
}

// StopContainer stops a running container.
func (c *Client) StopContainer(ctx context.Context, id string, timeout *int) error {
	var opts container.StopOptions
	if timeout != nil {
		opts.Timeout = timeout
	}
	if err := c.api.ContainerStop(ctx, id, opts); err != nil {
		return fmt.Errorf("stop %s: %w", shortID(id), err)
	}
	return nil
}

// RestartContainer restarts a container.
func (c *Client) RestartContainer(ctx context.Context, id string, timeout *int) error {
	var opts container.StopOptions
	if timeout != nil {
		opts.Timeout = timeout
	}
	if err := c.api.ContainerRestart(ctx, id, opts); err != nil {
		return fmt.Errorf("restart %s: %w", shortID(id), err)
	}
	return nil
}

// RemoveContainer removes a container.
func (c *Client) RemoveContainer(ctx context.Context, id string, force bool) error {
	opts := container.RemoveOptions{Force: force, RemoveVolumes: false}
	if err := c.api.ContainerRemove(ctx, id, opts); err != nil {
		return fmt.Errorf("remove %s: %w", shortID(id), err)
	}
	return nil
}

// ListNetworks returns networks on the host.
func (c *Client) ListNetworks(ctx context.Context) ([]model.Network, error) {
	items, err := c.api.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}
	out := make([]model.Network, 0, len(items))
	hostName := c.host.Name
	for _, it := range items {
		out = append(out, model.Network{
			ID:         it.ID,
			Name:       it.Name,
			Driver:     it.Driver,
			Scope:      it.Scope,
			Containers: len(it.Containers),
			Host:       hostName,
		})
	}
	return out, nil
}

// RemoveNetwork removes a network by ID or name.
func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	if err := c.api.NetworkRemove(ctx, id); err != nil {
		return fmt.Errorf("remove network %s: %w", shortID(id), err)
	}
	return nil
}

// ListVolumes returns volumes on the host.
func (c *Client) ListVolumes(ctx context.Context) ([]model.Volume, error) {
	resp, err := c.api.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}
	out := make([]model.Volume, 0, len(resp.Volumes))
	hostName := c.host.Name
	for _, it := range resp.Volumes {
		out = append(out, model.Volume{
			Name:       it.Name,
			Driver:     it.Driver,
			Mountpoint: it.Mountpoint,
			Host:       hostName,
		})
	}
	return out, nil
}

// RemoveVolume removes a volume by name.
func (c *Client) RemoveVolume(ctx context.Context, name string, force bool) error {
	if err := c.api.VolumeRemove(ctx, name, force); err != nil {
		return fmt.Errorf("remove volume %s: %w", name, err)
	}
	return nil
}

// ListImages returns images on the host.
func (c *Client) ListImages(ctx context.Context) ([]model.Image, error) {
	items, err := c.api.ImageList(ctx, image.ListOptions{All: false})
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	out := make([]model.Image, 0, len(items))
	hostName := c.host.Name
	for _, it := range items {
		tags := append([]string(nil), it.RepoTags...)
		out = append(out, model.Image{
			ID:      it.ID,
			Tags:    tags,
			Size:    it.Size,
			Created: time.Unix(it.Created, 0).UTC(),
			Host:    hostName,
		})
	}
	return out, nil
}

// RemoveImage removes an image by ID or reference.
func (c *Client) RemoveImage(ctx context.Context, id string, force bool) error {
	_, err := c.api.ImageRemove(ctx, id, image.RemoveOptions{Force: force})
	if err != nil {
		return fmt.Errorf("remove image %s: %w", shortID(id), err)
	}
	return nil
}

// ListContexts reads Docker CLI contexts from the config directory.
func ListContexts() ([]DockerContext, error) {
	configDir, err := dockerConfigDir()
	if err != nil {
		return nil, err
	}

	metaRoot := filepath.Join(configDir, "contexts", "meta")
	entries, err := os.ReadDir(metaRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []DockerContext{{Name: "default", Current: true}}, nil
		}
		return nil, fmt.Errorf("read contexts: %w", err)
	}

	current, _ := CurrentContextName()
	if current == "" {
		current = "default"
	}

	out := make([]DockerContext, 0, len(entries)+1)
	seenDefault := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(metaRoot, e.Name(), "meta.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var meta contextMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		eps := make(map[string]string, len(meta.Endpoints))
		for k, v := range meta.Endpoints {
			eps[k] = v.Host
		}
		if meta.Name == "default" {
			seenDefault = true
		}
		out = append(out, DockerContext{
			Name:      meta.Name,
			Current:   meta.Name == current,
			Endpoints: eps,
		})
	}
	if !seenDefault {
		out = append(out, DockerContext{
			Name:    "default",
			Current: current == "default",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CurrentContextName returns the active Docker CLI context name.
func CurrentContextName() (string, error) {
	configDir, err := dockerConfigDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(configDir, "config.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return "default", nil
		}
		return "", fmt.Errorf("read docker config: %w", err)
	}
	var cfg struct {
		CurrentContext string `json:"currentContext"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse docker config: %w", err)
	}
	if cfg.CurrentContext == "" {
		return "default", nil
	}
	return cfg.CurrentContext, nil
}

type contextMeta struct {
	Name      string                     `json:"Name"`
	Endpoints map[string]contextEndpoint `json:"Endpoints"`
}

type contextEndpoint struct {
	Host string `json:"Host"`
}

func dockerConfigDir() (string, error) {
	if v := os.Getenv("DOCKER_CONFIG"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".docker"), nil
}

func trimContainerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	n := names[0]
	return strings.TrimPrefix(n, "/")
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
