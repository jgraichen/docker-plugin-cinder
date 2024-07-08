package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/gophercloud/gophercloud/v2/pagination"
)

type plugin struct {
	blockClient   *gophercloud.ServiceClient
	computeClient *gophercloud.ServiceClient
	config        *tConfig
	mutex         *sync.Mutex
}

func newPlugin(provider *gophercloud.ProviderClient, endpointOpts gophercloud.EndpointOpts, config *tConfig) (*plugin, error) {
	blockClient, err := openstack.NewBlockStorageV3(provider, endpointOpts)

	if err != nil {
		return nil, err
	}

	computeClient, err := openstack.NewComputeV2(provider, endpointOpts)

	if err != nil {
		return nil, err
	}

	if len(config.MachineID) == 0 {
		bytes, err := ioutil.ReadFile("/etc/machine-id")
		if err != nil {
			log.WithError(err).Error("Error reading machine id")
			return nil, err
		}

		uuid, err := uuid.FromString(strings.TrimSpace(string(bytes)))
		if err != nil {
			log.WithError(err).Error("Error parsing machine id")
			return nil, err
		}

		log.WithField("id", uuid).Info("Machine ID detected")
		config.MachineID = uuid.String()
	} else {
		log.WithField("id", config.MachineID).Debug("Using configured machine ID")
	}

	return &plugin{
		blockClient:   blockClient,
		computeClient: computeClient,
		config:        config,
		mutex:         &sync.Mutex{},
	}, nil
}

func (d plugin) Capabilities() *volume.CapabilitiesResponse {
	return &volume.CapabilitiesResponse{
		Capabilities: volume.Capability{Scope: "global"},
	}
}

func (d plugin) Create(r *volume.CreateRequest) error {
	logger := log.WithFields(log.Fields{"name": r.Name, "action": "create"})
	logger.Infof("Creating volume '%s' ...", r.Name)
	logger.Debugf("Create: %+v", r)

	d.mutex.Lock()
	defer d.mutex.Unlock()

	// DEFAULT SIZE IN GB
	var size = 10
	var err error

	if s, ok := r.Options["size"]; ok {
		size, err = strconv.Atoi(s)
		if err != nil {
			logger.WithError(err).Error("Error parsing size option")
			return fmt.Errorf("Invalid size option: %s", err.Error())
		}
	}

	vol, err := volumes.Create(context.TODO(), d.blockClient, volumes.CreateOpts{
		Size: size,
		Name: r.Name,
	}, volumes.SchedulerHintOpts{}).Extract()

	if err != nil {
		logger.WithError(err).Errorf("Error creating volume: %s", err.Error())
		return err
	}

	logger.WithField("id", vol.ID).Debug("Volume created")

	return nil
}

func (d plugin) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	logger := log.WithFields(log.Fields{"name": r.Name, "action": "get"})
	logger.Debugf("Get: %+v", r)

	vol, err := d.getByName(r.Name)

	if err != nil {
		logger.WithError(err).Errorf("Error retriving volume: %s", err.Error())
		return nil, err
	}

	response := &volume.GetResponse{
		Volume: &volume.Volume{
			Name:       r.Name,
			CreatedAt:  vol.CreatedAt.Format(time.RFC3339),
			Mountpoint: filepath.Join(d.config.MountDir, r.Name),
		},
	}

	return response, nil
}

func (d plugin) List() (*volume.ListResponse, error) {
	logger := log.WithFields(log.Fields{"action": "list"})
	logger.Debugf("List")

	var vols []*volume.Volume

	pager := volumes.List(d.blockClient, volumes.ListOpts{})
	err := pager.EachPage(context.TODO(), func(ctx context.Context, page pagination.Page) (bool, error) {
		vList, _ := volumes.ExtractVolumes(page)

		for _, v := range vList {
			if len(v.Name) > 0 {
				vols = append(vols, &volume.Volume{
					Name:      v.Name,
					CreatedAt: v.CreatedAt.Format(time.RFC3339),
				})
			}
		}

		return true, nil
	})

	if err != nil {
		logger.WithError(err).Errorf("Error listing volume: %s", err.Error())
		return nil, err
	}

	return &volume.ListResponse{Volumes: vols}, nil
}

func (d plugin) Mount(r *volume.MountRequest) (*volume.MountResponse, error) {
	logger := log.WithFields(log.Fields{"name": r.Name, "action": "mount"})
	logger.Infof("Mounting volume '%s' ...", r.Name)
	logger.Debugf("Mount: %+v", r)

	d.mutex.Lock()
	defer d.mutex.Unlock()

	vol, err := d.getByName(r.Name)
	if err != nil {
		logger.WithError(err).Errorf("Error retriving volume: %s", err.Error())
		return nil, err
	}

	logger = logger.WithField("id", vol.ID)

	if vol.Status == "creating" || vol.Status == "detaching" {
		logger.Infof("Volume is in '%s' state, wait for 'available'...", vol.Status)
		if vol, err = d.waitOnVolumeState(logger.Context, vol, "available"); err != nil {
			logger.Error(err.Error())
			return nil, err
		}
	}

	if vol, err = volumes.Get(context.TODO(), d.blockClient, vol.ID).Extract(); err != nil {
		return nil, err
	}

	if len(vol.Attachments) > 0 {
		logger.Debug("Volume already attached, detaching first")
		if vol, err = d.detachVolume(logger.Context, vol); err != nil {
			logger.WithError(err).Error("Error detaching volume")
			return nil, err
		}

		if vol, err = d.waitOnVolumeState(logger.Context, vol, "available"); err != nil {
			logger.WithError(err).Error("Error detaching volume")
			return nil, err
		}
	}

	if vol.Status != "available" {
		logger.Debugf("Volume: %+v\n", vol)
		logger.Errorf("Invalid volume state for mounting: %s", vol.Status)
		return nil, errors.New("Invalid Volume State")
	}

	//
	// Attaching block volume to compute instance

	opts := volumeattach.CreateOpts{VolumeID: vol.ID}
	_, err = volumeattach.Create(context.TODO(), d.computeClient, d.config.MachineID, opts).Extract()

	if err != nil {
		logger.WithError(err).Errorf("Error attaching volume: %s", err.Error())
		return nil, err
	}

	//
	// Waiting for device appearance

	dev := fmt.Sprintf("/dev/disk/by-id/virtio-%.20s", vol.ID)
	logger.WithField("dev", dev).Debug("Waiting for device to appear...")
	err = waitForDevice(dev)

	if err != nil {
		logger.WithError(err).Error("Expected block device not found")
		return nil, fmt.Errorf("Block device not found: %s", dev)
	}

	//
	// Check filesystem and format if necessary

	fsType, err := getFilesystemType(dev)
	if err != nil {
		logger.WithError(err).Error("Detecting filesystem type failed")
		return nil, err
	}

	if fsType == "" {
		logger.Debug("Volume is empty, formatting")
		if err := formatFilesystem(dev, r.Name); err != nil {
			logger.WithError(err).Error("Formatting failed")
			return nil, err
		}
	}

	//
	// Mount device

	path := filepath.Join(d.config.MountDir, r.Name)
	if err = os.MkdirAll(path, 0700); err != nil {
		logger.WithError(err).Error("Error creating mount directory")
		return nil, err
	}

	logger.WithField("mount", path).Debug("Mounting volume...")
	out, err := exec.Command("mount", dev, path).CombinedOutput()
	if err != nil {
		log.WithError(err).Errorf("%s", out)
		return nil, errors.New(string(out))
	}

	resp := volume.MountResponse{
		Mountpoint: path,
	}

	logger.Debug("Volume successfully mounted")

	return &resp, nil
}

func (d plugin) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	logger := log.WithFields(log.Fields{"name": r.Name, "action": "path"})
	logger.Debugf("Path: %+v", r)

	resp := volume.PathResponse{
		Mountpoint: filepath.Join(d.config.MountDir, r.Name),
	}

	return &resp, nil
}

func (d plugin) Remove(r *volume.RemoveRequest) error {
	logger := log.WithFields(log.Fields{"name": r.Name, "action": "remove"})
	logger.Infof("Removing volume '%s' ...", r.Name)
	logger.Debugf("Remove: %+v", r)

	vol, err := d.getByName(r.Name)

	if err != nil {
		logger.WithError(err).Errorf("Error retriving volume: %s", err.Error())
		return err
	}

	logger = logger.WithField("id", vol.ID)

	if len(vol.Attachments) > 0 {
		logger.Debug("Volume still attached, detaching first")
		if vol, err = d.detachVolume(logger.Context, vol); err != nil {
			logger.WithError(err).Error("Error detaching volume")
			return err
		}
	}

	logger.Debug("Deleting block volume...")

	err = volumes.Delete(context.TODO(), d.blockClient, vol.ID, volumes.DeleteOpts{}).ExtractErr()
	if err != nil {
		logger.WithError(err).Errorf("Error deleting volume: %s", err.Error())
		return err
	}

	logger.Debug("Volume deleted")

	return nil
}

func (d plugin) Unmount(r *volume.UnmountRequest) error {
	logger := log.WithFields(log.Fields{"name": r.Name, "action": "unmount"})
	logger.Infof("Unmounting volume '%s' ...", r.Name)
	logger.Debugf("Unmount: %+v", r)

	d.mutex.Lock()
	defer d.mutex.Unlock()

	path := filepath.Join(d.config.MountDir, r.Name)
	exists, err := isDirectoryPresent(path)
	if err != nil {
		logger.WithError(err).Error("Error checking directory stat: %s", path)
	}

	if exists {
		err = syscall.Unmount(path, 0)
		if err != nil {
			logger.WithError(err).Errorf("Error unmount %s", path)
		}
	}

	vol, err := d.getByName(r.Name)
	if err != nil {
		logger.WithError(err).Error("Error retriving volume")
	} else {
		_, err = d.detachVolume(logger.Context, vol)
		if err != nil {
			logger.WithError(err).Error("Error detaching volume")
		}
	}

	return nil
}

func (d plugin) getByName(name string) (*volumes.Volume, error) {
	var volume *volumes.Volume

	pager := volumes.List(d.blockClient, volumes.ListOpts{Name: name})
	err := pager.EachPage(context.TODO(), func(ctx context.Context, page pagination.Page) (bool, error) {
		vList, err := volumes.ExtractVolumes(page)

		if err != nil {
			return false, err
		}

		for _, v := range vList {
			if v.Name == name {
				volume = &v
				return false, nil
			}
		}

		return true, nil
	})

	if len(volume.ID) == 0 {
		return nil, errors.New("Not Found")
	}

	return volume, err
}

func (d plugin) detachVolume(ctx context.Context, vol *volumes.Volume) (*volumes.Volume, error) {
	for _, att := range vol.Attachments {
		err := volumeattach.Delete(ctx, d.computeClient, att.ServerID, att.ID).ExtractErr()
		if err != nil {
			return nil, err
		}
	}

	return vol, nil
}

func (d plugin) waitOnVolumeState(ctx context.Context, vol *volumes.Volume, status string) (*volumes.Volume, error) {
	if vol.Status == status {
		return vol, nil
	}

	for i := 1; i <= 10; i++ {
		time.Sleep(500 * time.Millisecond)

		vol, err := volumes.Get(ctx, d.blockClient, vol.ID).Extract()
		if err != nil {
			return nil, err
		}

		if vol.Status == status {
			return vol, nil
		}
	}

	log.WithContext(ctx).Debugf("Volume did not become %s: %+v", status, vol)

	return nil, fmt.Errorf("Volume status did became %s", status)
}
