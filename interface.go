package fleetingincus

import (
	"context"
	"encoding/json"
	"fmt"
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

	IncusImage            string `json:"incus_image"`
	IncusNamingScheme     string `json:"incus_naming_scheme"`
	IncusInstanceKeyPath  string `json:"incus_instance_key_path"`
	IncusInstanceSize     string `json:"incus_instance_size"`
	IncusDiskSize         string `json:"incus_disk_size"`         // Disk size for VMs (e.g., "100GiB")
	IncusStartupTimeout   int    `json:"incus_startup_timeout"`   // Timeout in seconds for VM startup
	IncusOperationTimeout int    `json:"incus_operation_timeout"` // Timeout in seconds for Incus operations
	MaxInstances          int    `json:"max_instances"`
	StateFilePath         string `json:"state_file_path"`

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

	g.log.Info("🚀 [INIT] Plugin initialization", "action", "start")

	// Setup state file path
	if g.StateFilePath == "" {
		g.StateFilePath = "/var/lib/fleeting-plugin-incus/state.json"
	}
	g.log.Debug("📁 [INIT] Configuring state file", "path", g.StateFilePath)

	// Create state directory and load existing state
	os.MkdirAll(filepath.Dir(g.StateFilePath), 0755)
	g.status, _ = load(g.StateFilePath)
	g.log.Info("💾 [INIT] State file loaded", "existing_vms", len(g.status))

	// Set default configuration values
	if g.IncusNamingScheme == "" {
		g.IncusNamingScheme = "runner-$random"
	}
	if g.IncusImage == "" {
		g.IncusImage = "runner-base" // Use local base image by default
	}
	if g.IncusInstanceSize == "" {
		g.IncusInstanceSize = "c1-m2" // 1 CPU, 1GB RAM - more descriptive than t2.micro
	}
	if g.IncusDiskSize == "" {
		g.IncusDiskSize = "10GiB" // Default 100GB root disk
	}
	if g.IncusStartupTimeout == 0 {
		g.IncusStartupTimeout = 120 // 2 minutes for VM startup
	}
	if g.IncusOperationTimeout == 0 {
		g.IncusOperationTimeout = 60 // 1 minute for Incus operations
	}
	if g.MaxInstances == 0 {
		g.MaxInstances = 20 // More reasonable default
	}

	// Validate configuration
	if g.IncusInstanceKeyPath == "" {
		g.log.Warn("⚠️ [INIT] SSH key path not configured", "config_key", "incus_instance_key_path")
	} else {
		if _, err := os.Stat(g.IncusInstanceKeyPath); os.IsNotExist(err) {
			g.log.Error("❌ [INIT] SSH key file not found", "path", g.IncusInstanceKeyPath)
			return provider.ProviderInfo{}, fmt.Errorf("SSH key file not found: %s", g.IncusInstanceKeyPath)
		}
	}

	g.log.Info("⚙️ [INIT] Configuration validated",
		"image", g.IncusImage,
		"size", g.IncusInstanceSize,
		"disk_size", g.IncusDiskSize,
		"naming_scheme", g.IncusNamingScheme,
		"max_instances", g.MaxInstances,
		"startup_timeout", g.IncusStartupTimeout,
		"operation_timeout", g.IncusOperationTimeout,
		"ssh_key_path", g.IncusInstanceKeyPath)

	// Connect to Incus
	g.log.Info("🔌 [INIT] Connecting to Incus daemon")
	err = incusprov.ConnectIncus()
	if err != nil {
		g.log.Error("❌ [INIT] Incus connection failed", "error", err)
		return provider.ProviderInfo{}, err
	}

	g.log.Info("✅ [INIT] Plugin ready",
		"provider_id", ProviderID,
		"version", Version,
		"max_instances", g.MaxInstances,
		"state_file", g.StateFilePath)

	return provider.ProviderInfo{
		ID:        ProviderID,
		MaxSize:   g.MaxInstances, // Use configured max instances
		Version:   Version,
		BuildInfo: BuildInfo,
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
	g.log.Info("🔍 [CONNECT] Connection info request", "vm_name", name)

	g.m.Lock()
	info := provider.ConnectInfo{ConnectorConfig: g.settings.ConnectorConfig}
	keyPath := g.IncusInstanceKeyPath
	g.m.Unlock()

	g.log.Debug("📋 [CONNECT] Gathering VM network information", "vm_name", name)
	ip, err := incusprov.GetVM(name)
	if err != nil {
		g.log.Error("❌ [CONNECT] Failed to get VM network information", "vm_name", name, "error", err)
		return provider.ConnectInfo{}, err
	}

	g.log.Debug("🔑 [CONNECT] Loading SSH key", "key_path", keyPath)
	info.OS = "linux"
	info.Arch = "amd64"
	info.Protocol = provider.ProtocolSSH
	info.UseStaticCredentials = true
	info.Username = "root"
	info.Key, err = os.ReadFile(keyPath)
	if err != nil {
		g.log.Error("❌ [CONNECT] Failed to read SSH key", "key_path", keyPath, "error", err)
		return provider.ConnectInfo{}, err
	}

	info.InternalAddr = ip
	info.ExternalAddr = ip

	g.log.Info("✅ [CONNECT] Connection info ready",
		"vm_name", name,
		"ip_address", ip,
		"protocol", "SSH",
		"username", "root")

	return info, nil
}

// Decrease removes the specified instances from the instance group. It
// returns instance IDs of successful requests for removal.
func (g *InstanceGroup) Decrease(ctx context.Context, instances []string) (removed []string, err error) {
	g.log.Info("📉 [DELETE] Scale down request", "vms_to_delete", len(instances), "vm_names", instances)

	for i, name := range instances {
		vmNumber := i + 1
		g.log.Info("🗑️ [DELETE] Processing VM",
			"vm_name", name,
			"progress", fmt.Sprintf("%d/%d", vmNumber, len(instances)))

		// First check if VM exists in Incus
		_, vmErr := incusprov.GetVM(name)
		if vmErr != nil {
			g.log.Info("👻 [DELETE] VM not found in Incus (already deleted)", "vm_name", name)

			// VM doesn't exist, just mark as deleted in our state
			g.m.Lock()
			g.status[name] = provider.StateDeleted
			save(g.StateFilePath, g.status)
			g.m.Unlock()

			removed = append(removed, name)
			continue
		}

		// VM exists, try to delete it
		g.log.Info("⏹️ [DELETE] Stopping and deleting VM", "vm_name", name)
		deleteErr := incusprov.DeleteVM(name)
		if deleteErr != nil {
			g.log.Error("❌ [DELETE] VM deletion failed",
				"vm_name", name,
				"progress", fmt.Sprintf("%d/%d", vmNumber, len(instances)),
				"error", deleteErr)
			continue
		}

		// Mark as deleted in state
		g.m.Lock()
		g.status[name] = provider.StateDeleted
		save(g.StateFilePath, g.status)
		g.m.Unlock()

		g.log.Info("✅ [DELETE] VM deletion completed",
			"vm_name", name,
			"progress", fmt.Sprintf("%d/%d", vmNumber, len(instances)))

		removed = append(removed, name)
	}

	// Final result logging
	if len(removed) == len(instances) {
		g.log.Info("✅ [DELETE] Scale down completed",
			"requested", len(instances),
			"deleted", len(removed))
	} else if len(removed) > 0 {
		g.log.Warn("⚠️ [DELETE] Partial scale down",
			"requested", len(instances),
			"deleted", len(removed),
			"failed", len(instances)-len(removed))
	} else {
		g.log.Error("❌ [DELETE] Scale down failed",
			"requested", len(instances),
			"deleted", 0)
	}

	err = nil
	return
}

// Increase requests more instances to be created. It returns how many
// instances were successfully requested.
func (g *InstanceGroup) Increase(ctx context.Context, delta int) (success int, err error) {
	g.log.Info("📈 [CREATE] Scale up request", "vms_to_create", delta)

	g.m.Lock()
	originalDelta := delta
	creatingCount := 0

	// Count existing VMs in creating state
	for id := range g.status {
		if g.status[id] == provider.StateCreating {
			g.log.Debug("⏳ [ANALYSIS] Found VM in creating state", "vm_name", id)
			delta--
			creatingCount++
		}
	}

	// Log current state analysis
	totalVMs := len(g.status)
	g.log.Info("📊 [ANALYSIS] State analysis",
		"total_vms", totalVMs,
		"vms_creating", creatingCount,
		"requested", originalDelta,
		"will_create", delta)

	namingScheme := g.IncusNamingScheme
	instanceSize := g.IncusInstanceSize
	instanceImage := g.IncusImage
	startupTimeout := g.IncusStartupTimeout
	g.m.Unlock()

	// Clean up stale StateCreating VMs that don't exist in Incus
	if creatingCount > 0 {
		g.log.Info("🧹 [CLEANUP] Checking for stale VMs", "vms_to_check", creatingCount)
		cleaned := g.cleanupStaleCreatingVMs()
		if cleaned > 0 {
			// Recalculate delta after cleanup
			delta = originalDelta - (creatingCount - cleaned)
			g.log.Info("♻️ [CLEANUP] Cleanup completed", "stale_vms_removed", cleaned, "new_delta", delta)
		}
	}

	// Early exit if no VMs to create
	if delta <= 0 {
		g.log.Info("⏸️ [ANALYSIS] No new VMs needed", "reason", "sufficient_existing_or_creating")
		return 0, nil
	}

	var lastErr error
	g.log.Info("🏗️ [CREATE] Starting VM creation", "vms_to_create", delta)

	for i := range delta {
		vmNumber := i + 1
		name := os.Expand(namingScheme, naming)

		g.log.Info("🔨 [CREATE] Creating VM",
			"vm_name", name,
			"progress", fmt.Sprintf("%d/%d", vmNumber, delta),
			"image", instanceImage,
			"size", instanceSize,
			"disk_size", g.IncusDiskSize)

		// Mark VM as creating
		g.m.Lock()
		g.status[name] = provider.StateCreating
		save(g.StateFilePath, g.status)
		g.m.Unlock()

		// Create the VM
		createErr := incusprov.CreateVMWithDiskSize(name, instanceSize, instanceImage, startupTimeout, g.IncusDiskSize)
		if createErr != nil {
			g.log.Error("❌ [CREATE] VM creation failed",
				"vm_name", name,
				"progress", fmt.Sprintf("%d/%d", vmNumber, delta),
				"error", createErr)

			// Clean up failed VM from state
			g.m.Lock()
			delete(g.status, name)
			save(g.StateFilePath, g.status)
			g.m.Unlock()

			lastErr = createErr
			continue
		}

		// Mark VM as running
		g.m.Lock()
		g.status[name] = provider.StateRunning
		save(g.StateFilePath, g.status)
		g.m.Unlock()

		g.log.Info("✅ [CREATE] VM creation completed",
			"vm_name", name,
			"progress", fmt.Sprintf("%d/%d", vmNumber, delta))

		success++
	}

	// Final result logging
	if success == 0 && lastErr != nil {
		err = lastErr
		g.log.Error("❌ [CREATE] Scale up failed",
			"requested", originalDelta,
			"attempted", delta,
			"successful", success,
			"error", err)
	} else if success < delta {
		g.log.Warn("⚠️ [CREATE] Partial scale up",
			"requested", originalDelta,
			"attempted", delta,
			"successful", success,
			"failed", delta-success)
	} else {
		g.log.Info("✅ [CREATE] Scale up completed",
			"requested", originalDelta,
			"created", success)
	}

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

	g.log.Debug("🔍 [CLEANUP] Scanning for stale VMs")
	for id, state := range g.status {
		if state == provider.StateCreating {
			g.log.Debug("⏳ [CLEANUP] Checking VM state", "vm_name", id)
			// Check if VM actually exists in Incus
			_, err := incusprov.GetVM(id)
			if err != nil {
				g.log.Warn("👻 [CLEANUP] Found stale VM", "vm_name", id, "reason", "not_found_in_incus")
				toCleanup = append(toCleanup, id)
			} else {
				g.log.Debug("✓ [CLEANUP] VM exists in Incus", "vm_name", id)
			}
		}
	}

	// Clean up stale VMs
	if len(toCleanup) > 0 {
		g.log.Info("🧹 [CLEANUP] Cleaning up stale VMs", "vms_to_cleanup", len(toCleanup), "vm_names", toCleanup)
		for _, id := range toCleanup {
			delete(g.status, id)
			g.log.Debug("🗑️ [CLEANUP] Removed from state", "vm_name", id)
		}
		save(g.StateFilePath, g.status)
	} else {
		g.log.Debug("✓ [CLEANUP] No stale VMs found")
	}

	return len(toCleanup)
}
