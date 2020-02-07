package main

import (
	"errors"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/volumeattach"
	"github.com/gophercloud/gophercloud/pagination"
)

type plugin struct {
	blockClient   *gophercloud.ServiceClient
	computeClient *gophercloud.ServiceClient
	config        *tConfig
	mutex         *sync.Mutex
	instanceUUID  string
}

func newPlugin(provider *gophercloud.ProviderClient, endpointOpts gophercloud.EndpointOpts, config *tConfig) (plugin, error) {
	blockClient, err := openstack.NewBlockStorageV3(provider, endpointOpts)

	if err != nil {
		return plugin{}, err
	}

	computeClient, err := openstack.NewComputeV2(provider, endpointOpts)

	if err != nil {
		return plugin{}, err
	}

	// Detect host UUID here

	return plugin{
		blockClient:   blockClient,
		computeClient: computeClient,
		config:        config,
		mutex:         &sync.Mutex{},
		instanceUUID:  "3f1ffa4c-2f24-425b-85b6-b77b302fb70c",
	}, nil
}

func (d plugin) Capabilities() *volume.CapabilitiesResponse {
	return &volume.CapabilitiesResponse{
		Capabilities: volume.Capability{Scope: "global"},
	}
}

func (d plugin) Create(r *volume.CreateRequest) error {
	logger := log.WithFields(log.Fields{"volume": r.Name, "action": "create"})
	logger.Infof("Creating volume '%s' ...", r.Name)

	d.mutex.Lock()
	defer d.mutex.Unlock()

	vol, err := volumes.Create(d.blockClient, volumes.CreateOpts{
		Size: 10,
		Name: r.Name,
	}).Extract()

	if err != nil {
		logger.WithError(err).Errorf("Error creating volume: %s", err.Error())
		return err
	}

	logger.WithField("id", vol.ID).Debug("Volume created.")

	return nil
}

func (d plugin) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	logger := log.WithFields(log.Fields{"volume": r.Name, "action": "get"})
	vol, err := d.getByName(r.Name)

	if err != nil {
		logger.WithError(err).Errorf("Error retriving volume: %s", err.Error())
		return nil, err
	}

	response := &volume.GetResponse{
		Volume: &volume.Volume{
			Name:      r.Name,
			CreatedAt: vol.CreatedAt.Format(time.RFC3339),
		},
	}

	return response, nil
}

func (d plugin) List() (*volume.ListResponse, error) {
	logger := log.WithFields(log.Fields{"action": "list"})
	var vols []*volume.Volume

	pager := volumes.List(d.blockClient, volumes.ListOpts{})
	err := pager.EachPage(func(page pagination.Page) (bool, error) {
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
	logger := log.WithFields(log.Fields{"volume": r.Name, "action": "mount"})
	logger.Infof("Mounting volume '%s' ...", r.Name)

	d.mutex.Lock()
	defer d.mutex.Unlock()

	vol, err := d.getByName(r.Name)

	if err != nil {
		logger.WithError(err).Errorf("Error retriving volume: %s", err.Error())
		return nil, err
	}

	logger = logger.WithField("id", vol.ID)

	if vol.Status == "creating" {
		// Wait for volume creation as the docker API can be quite fast
		time.Sleep(5 * time.Second)
	}

	vol, err = volumes.Get(d.blockClient, vol.ID).Extract()

	if err != nil {
		logger.WithError(err).Errorf("Error retriving volume: %s", err.Error())
		return nil, err
	}

	if vol.Status != "available" {
		logger.Debugf("Volume: %+v\n", vol)
		logger.Errorf("Invalid volume state for mounting: %s", vol.Status)
		return nil, errors.New("Invalid Volume State")
	}

	_, err = volumeattach.Create(d.computeClient, d.instanceUUID, volumeattach.CreateOpts{
		VolumeID: vol.ID,
	}).Extract()

	if err != nil {
		logger.WithError(err).Errorf("Error attaching volume: %s", err.Error())
		return nil, err
	}

	return nil, errors.New("Not Implemented")
}

func (d plugin) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	return nil, errors.New("Not Implemented")
}

func (d plugin) Remove(r *volume.RemoveRequest) error {
	logger := log.WithFields(log.Fields{"volume": r.Name, "action": "remove"})
	logger.Infof("Remove volume '%s' ...", r.Name)

	vol, err := d.getByName(r.Name)

	if err != nil {
		logger.WithError(err).Errorf("Error retriving volume: %s", err.Error())
		return err
	}

	logger = logger.WithField("id", vol.ID)
	logger.Debug("Deleting volume...")

	err = volumes.Delete(d.blockClient, vol.ID, volumes.DeleteOpts{}).ExtractErr()
	if err != nil {
		logger.WithError(err).Errorf("Error deleting volume: %s", err.Error())
		return err
	}

	logger.Debug("Volume deleted.")

	return nil
}

func (d plugin) Unmount(r *volume.UnmountRequest) error {
	return errors.New("Not Implemented")
}

func (d plugin) getByName(name string) (volumes.Volume, error) {
	var volume volumes.Volume

	pager := volumes.List(d.blockClient, volumes.ListOpts{Name: name})
	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		vList, err := volumes.ExtractVolumes(page)

		if err != nil {
			return false, err
		}

		for _, v := range vList {
			if v.Name == name {
				volume = v
				return false, nil
			}
		}

		return true, nil
	})

	if len(volume.ID) == 0 {
		return volume, errors.New("Not Found")
	}

	return volume, err
}
