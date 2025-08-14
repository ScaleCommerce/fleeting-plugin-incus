package fleetingincus

import (
	"context"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"sync"

	"fleeting-plugin-incus/incusprov"

	"github.com/hashicorp/go-hclog"
	"gitlab.com/gitlab-org/fleeting/fleeting/provider"
)

var _ provider.InstanceGroup = (*InstanceGroup)(nil)

type InstanceGroup struct {
	m *sync.Mutex

	IncusImage           string `json:"incus_image"`
	IncusNamingScheme    string `json:"incus_naming_scheme"`
	IncusInstanceKeyPath string `json:"incus_instance_key_path"`
	IncusInstanceSize    string `json:"incus_instance_size"`
	MaxInstances         int    `json:"max_instances"`
	StateFilePath        string `json:"state_file_path"`

	log      hclog.Logger
	settings provider.Settings

	status map[string]provider.State
}

func (g *InstanceGroup) Init(ctx context.Context, log hclog.Logger, settings provider.Settings) (provider.ProviderInfo, error) {
	var err error

	g.m = &sync.Mutex{}

	g.m.Lock()
	defer g.m.Unlock()

	g.log = log
	g.settings = settings

	if g.StateFilePath == "" {
		g.StateFilePath = "/var/lib/fleeting-plugin-incus/state.json"
	}

	os.MkdirAll(filepath.Dir(g.StateFilePath), 0755)
	g.status, _ = load(g.StateFilePath)

	if g.IncusNamingScheme == "" {
		g.IncusNamingScheme = "runner-$random"
	}

	if g.IncusImage == "" {
		g.IncusImage = "ubuntu:22.04"
	}

	if g.IncusInstanceSize == "" {
		g.IncusInstanceSize = "t2.micro"
	}

	if g.MaxInstances == 0 {
		g.MaxInstances = 20
	}

	g.log.Info("Connecting to Incus")

	err = incusprov.ConnectIncus()
	if err != nil {
		g.log.Error("Failed to connect to Incus", "error", err)
		return provider.ProviderInfo{}, err
	}

	g.log.Info("Successfully connected to Incus", "image", g.IncusImage, "size", g.IncusInstanceSize, "naming", g.IncusNamingScheme)

	return provider.ProviderInfo{
		ID:        "incus",
		MaxSize:   10,
		Version:   "v0.1.0",
		BuildInfo: "0",
	}, nil
}

// Update updates instance data from the instance group, passing a function
// to perform instance reconciliation.
func (g *InstanceGroup) Update(ctx context.Context, update func(id string, state provider.State)) error {
	g.m.Lock()
	defer g.m.Unlock()

	for id, state := range g.status {
		update(id, state)
	}

	save(g.StateFilePath, g.status)

	return nil
}

// ConnectInfo returns additional information about an instance,
// useful for creating a connection.
func (g *InstanceGroup) ConnectInfo(ctx context.Context, name string) (provider.ConnectInfo, error) {
	g.log.Info("Getting VM info", "name", name)

	g.m.Lock()
	info := provider.ConnectInfo{ConnectorConfig: g.settings.ConnectorConfig}
	keyPath := g.IncusInstanceKeyPath
	g.m.Unlock()

	ip, err := incusprov.GetVM(name)
	if err != nil {
		return provider.ConnectInfo{}, err
	}

	info.OS = "linux"
	info.Arch = "amd64"
	info.Protocol = provider.ProtocolSSH
	info.UseStaticCredentials = true
	info.Username = "root"
	info.Key, err = os.ReadFile(keyPath)
	if err != nil {
		return provider.ConnectInfo{}, err
	}

	info.InternalAddr = ip
	info.ExternalAddr = ip

	g.log.Info("Returning VM info", "name", name, "ip", ip)

	return info, nil
}

// Decrease removes the specified instances from the instance group. It
// returns instance IDs of successful requests for removal.
func (g *InstanceGroup) Decrease(ctx context.Context, instances []string) (removed []string, err error) {
	g.log.Info("Deleting VMs", "instances", instances)

	for _, name := range instances {
		// First check if VM exists in Incus
		_, vmErr := incusprov.GetVM(name)
		if vmErr != nil {
			g.log.Info("VM already doesn't exist in Incus, marking as deleted", "name", name)
			// VM doesn't exist, just mark as deleted in our state
			g.m.Lock()
			g.status[name] = provider.StateDeleted
			save(g.StateFilePath, g.status)
			g.m.Unlock()
			removed = append(removed, name)
			continue
		}

		// VM exists, try to delete it
		deleteErr := incusprov.DeleteVM(name)
		if deleteErr != nil {
			g.log.Error("Failed to delete VM", "name", name, "error", deleteErr)
			continue
		}

		g.log.Info("Successfully deleted VM", "name", name)
		g.m.Lock()
		g.status[name] = provider.StateDeleted
		save(g.StateFilePath, g.status)
		g.m.Unlock()

		removed = append(removed, name)
	}
	err = nil
	return
}

// Increase requests more instances to be created. It returns how many
// instances were successfully requested.
func (g *InstanceGroup) Increase(ctx context.Context, delta int) (success int, err error) {
	g.log.Info("Requesting VM creation", "request_delta", delta)

	g.m.Lock()
	originalDelta := delta
	creatingCount := 0
	for id := range g.status {
		if g.status[id] == provider.StateCreating {
			g.log.Debug("Found VM in StateCreating", "vm", id, "status", g.status[id])
			delta--
			creatingCount++
		}
	}

	g.log.Info("VM state analysis", "original_delta", originalDelta, "creating_vms", creatingCount, "adjusted_delta", delta)

	namingScheme := g.IncusNamingScheme
	instanceSize := g.IncusInstanceSize
	instanceImage := g.IncusImage

	g.m.Unlock()

	// Clean up stale StateCreating VMs that don't exist in Incus
	if creatingCount > 0 {
		cleaned := g.cleanupStaleCreatingVMs()
		if cleaned > 0 {
			// Recalculate delta after cleanup
			delta = originalDelta - (creatingCount - cleaned)
			g.log.Info("Recalculated delta after cleanup", "new_delta", delta, "cleaned", cleaned)
		}
	}

	var lastErr error
	for i := range delta {
		name := os.Expand(namingScheme, naming)

		g.m.Lock()
		g.status[name] = provider.StateCreating
		save(g.StateFilePath, g.status)
		g.m.Unlock()

		g.log.Info("Creating VM", "name", name, "size", instanceSize, "image", instanceImage)

		createErr := incusprov.CreateVM(name, instanceSize, instanceImage)
		if createErr != nil {
			g.log.Error("Failed to create VM", "name", name, "number", i, "request_delta", delta, "error", createErr)

			// Mark as failed and clean up
			g.m.Lock()
			delete(g.status, name)
			save(g.StateFilePath, g.status)
			g.m.Unlock()

			lastErr = createErr
			continue
		}

		g.log.Info("Successfully created VM", "name", name, "number", i, "request_delta", delta)

		g.m.Lock()
		g.status[name] = provider.StateRunning
		save(g.StateFilePath, g.status)
		g.m.Unlock()

		success++
	}

	// Return error if no VMs were created successfully
	if success == 0 && lastErr != nil {
		err = lastErr
		g.log.Error("Failed to create any VMs", "request_delta", delta, "error", err)
	}

	g.log.Info("VM creation completed", "requested", delta, "successful", success)
	return
}

// Shutdown performs any cleanup tasks required when the plugin is to shutdown.
func (g *InstanceGroup) Shutdown(ctx context.Context) error {
	g.m.Lock()
	defer g.m.Unlock()

	for id, state := range g.status {
		if state == provider.StateDeleted {
			delete(g.status, id)
		}
	}
	save(g.StateFilePath, g.status)

	return nil
}

func naming(i string) string {
	switch i {
	case "random":
		return randomString(10)
	}
	return ""
}

func randomString(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

func load(path string) (state map[string]provider.State, err error) {
	state = make(map[string]provider.State)

	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	json.Unmarshal(data, &state)
	return
}

func save(path string, state map[string]provider.State) (err error) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}

	err = os.WriteFile(path, data, 0644)
	return
}

// cleanupStaleCreatingVMs removes VMs that are marked as StateCreating but don't exist in Incus
func (g *InstanceGroup) cleanupStaleCreatingVMs() int {
	g.m.Lock()
	defer g.m.Unlock()

	var toCleanup []string

	for id, state := range g.status {
		if state == provider.StateCreating {
			// Check if VM actually exists in Incus
			_, err := incusprov.GetVM(id)
			if err != nil {
				g.log.Warn("Found stale StateCreating VM, cleaning up", "vm", id, "error", err)
				toCleanup = append(toCleanup, id)
			}
		}
	}

	// Clean up stale VMs
	for _, id := range toCleanup {
		delete(g.status, id)
		g.log.Info("Cleaned up stale StateCreating VM", "vm", id)
	}

	if len(toCleanup) > 0 {
		save(g.StateFilePath, g.status)
		g.log.Info("Cleaned up stale VMs", "count", len(toCleanup))
	}

	return len(toCleanup)
}
