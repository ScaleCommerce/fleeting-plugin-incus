package incusprov

import (
	"fmt"
	"slices"
	"time"

	incus "github.com/lxc/incus/client"
	"github.com/lxc/incus/shared/api"
)

var (
	ic incus.InstanceServer
)

func ConnectIncus() (err error) {
	ic, err = incus.ConnectIncusUnix("", nil)
	if err != nil {
		return fmt.Errorf("🔌 [INIT] failed to connect to incus daemon: %w", err)
	}

	return nil
}

func CreateVM(name, size, alias string) (err error) {
	return CreateVMWithTimeout(name, size, alias, 120) // Default 2 minutes
}

func CreateVMWithTimeout(name, size, alias string, timeoutSeconds int) (err error) {
	return CreateVMWithDiskSize(name, size, alias, timeoutSeconds, "100GiB")
}

func CreateVMWithDiskSize(name, size, alias string, timeoutSeconds int, diskSize string) (err error) {
	req := api.InstancesPost{
		Name: name,
		Source: api.InstanceSource{
			Type:  "image",
			Alias: alias,
		},
		Type:         "virtual-machine",
		InstanceType: size,
		Start:        true,
		InstancePut: api.InstancePut{
			Devices: map[string]map[string]string{
				"root": {
					"type": "disk",
					"path": "/",
					"pool": "default",
					"size": diskSize,
				},
			},
		},
	}

	// Create the instance
	op, err := ic.CreateInstance(req)
	if err != nil {
		return fmt.Errorf("🔨 [CREATE] failed to create VM '%s' with image '%s': %w", name, alias, err)
	}

	// Wait for creation to complete
	err = op.Wait()
	if err != nil {
		return fmt.Errorf("⏰ [CREATE] failed to wait for VM '%s' creation: %w", name, err)
	}

	// Wait for system to be ready
	maxRetries := timeoutSeconds / 2 // Check every 2 seconds
	for retry := 1; retry <= maxRetries; retry++ {
		time.Sleep(2 * time.Second)

		op, err = ic.ExecInstance(name, api.InstanceExecPost{
			Command:   []string{"systemctl", "is-system-running", "--wait"},
			WaitForWS: true,
		}, nil)

		if err != nil && err.Error() == "VM agent isn't currently running" {
			// VM agent not ready yet, continue waiting
			continue
		} else if err != nil {
			return fmt.Errorf("⚠️ [CREATE] failed to check system status for VM '%s' (retry %d/%d): %w", name, retry, maxRetries, err)
		}

		err = op.Wait()
		if err != nil && err.Error() == "VM agent isn't currently running" {
			// VM agent not ready yet, continue waiting
			continue
		} else if err != nil {
			return fmt.Errorf("⚠️ [CREATE] system not ready for VM '%s' (retry %d/%d): %w", name, retry, maxRetries, err)
		}

		// System is ready
		break
	}

	return
}

func DeleteVM(name string) (err error) {
	// First check if instance exists
	_, _, err = ic.GetInstanceFull(name)
	if err != nil {
		return fmt.Errorf("🔍 [DELETE] failed to find VM '%s': %w", name, err)
	}

	// Stop the instance
	reqState := api.InstanceStatePut{
		Action:  "stop",
		Timeout: -1,
	}

	op, err := ic.UpdateInstanceState(name, reqState, "")
	if err != nil {
		return fmt.Errorf("⏹️ [DELETE] failed to stop VM '%s': %w", name, err)
	}

	err = op.Wait()
	if err != nil {
		return fmt.Errorf("⏰ [DELETE] failed to wait for VM '%s' to stop: %w", name, err)
	}

	// Delete the instance
	op, err = ic.DeleteInstance(name)
	if err != nil {
		return fmt.Errorf("🗑️ [DELETE] failed to delete VM '%s': %w", name, err)
	}

	err = op.Wait()
	if err != nil {
		return fmt.Errorf("⏰ [DELETE] failed to wait for VM '%s' deletion: %w", name, err)
	}

	return
}

func GetVM(name string) (internalIP string, err error) {
	inst, _, err := ic.GetInstanceFull(name)
	if err != nil {
		err = fmt.Errorf("🔍 [CONNECT] failed to get VM info for '%s': %w", name, err)
		return
	}

	if !inst.IsActive() {
		err = fmt.Errorf("🚫 [CONNECT] VM '%s' is not active", name)
		return
	}

	if inst.State == nil || inst.State.Network == nil {
		err = fmt.Errorf("🌐 [CONNECT] no network information available for VM '%s'", name)
		return
	}

	// Find the primary IP address
	for netName, net := range inst.State.Network {
		if net.Type == "loopback" {
			continue
		}

		for _, addr := range net.Addresses {
			if slices.Contains([]string{"link", "local"}, addr.Scope) {
				continue
			}

			if addr.Family == "inet" && netName != "docker0" {
				internalIP = addr.Address
				return
			}
		}
	}

	err = fmt.Errorf("🌐 [CONNECT] no suitable IP address found for VM '%s'", name)
	return
}
