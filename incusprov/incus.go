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
		return fmt.Errorf("failed to connect to incus: %w", err)
	}

	return nil
}

func CreateVM(name, size, alias string) (err error) {
	req := api.InstancesPost{
		Name: name,
		Source: api.InstanceSource{
			Type:  "image",
			Alias: alias,
		},
		Type:         "virtual-machine",
		InstanceType: size,
		Start:        true,
	}

	op, err := ic.CreateInstance(req)
	if err != nil {
		return fmt.Errorf("failed to create instance '%s' with image '%s': %w", name, alias, err)
	}

	// Wait for the operation to complete
	err = op.Wait()
	if err != nil {
		return fmt.Errorf("failed to wait for instance '%s' creation: %w", name, err)
	}

	for {
		time.Sleep(2 * time.Second)

		op, err = ic.ExecInstance(name, api.InstanceExecPost{
			Command:   []string{"systemctl", "is-system-running", "--wait"},
			WaitForWS: true,
		}, nil)
		if err != nil && err.Error() == "VM agent isn't currently running" {
			continue
		} else if err != nil {
			err = fmt.Errorf("failed to wait for instance to start: %w", err)
			return
		}

		err = op.Wait()
		if err != nil && err.Error() == "VM agent isn't currently running" {
			continue
		} else if err != nil {
			err = fmt.Errorf("failed to wait for instance to start: %w", err)
			return
		}
		break
	}

	return
}

func DeleteVM(name string) (err error) {
	// First check if instance exists
	_, _, err = ic.GetInstanceFull(name)
	if err != nil {
		return fmt.Errorf("failed to get instance '%s': %w", name, err)
	}

	reqState := api.InstanceStatePut{
		Action:  "stop",
		Timeout: -1,
	}

	op, err := ic.UpdateInstanceState(name, reqState, "")
	if err != nil {
		return fmt.Errorf("failed to stop instance '%s': %w", name, err)
	}

	err = op.Wait()
	if err != nil {
		return fmt.Errorf("failed to stop instance '%s': %w", name, err)
	}

	op, err = ic.DeleteInstance(name)
	if err != nil {
		return fmt.Errorf("failed to delete instance '%s': %w", name, err)
	}

	err = op.Wait()
	if err != nil {
		return fmt.Errorf("failed to delete instance '%s': %w", name, err)
	}

	return
}

func GetVM(name string) (internalIP string, err error) {
	inst, _, err := ic.GetInstanceFull(name)
	if err != nil {
		err = fmt.Errorf("failed to get instance: %w", err)
		return
	}

	if inst.IsActive() && inst.State != nil && inst.State.Network != nil {
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
	}

	return
}
