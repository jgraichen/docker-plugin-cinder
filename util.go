package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

func getFilesystemType(dev string) (string, error) {
	out, err := exec.Command("blkid", "-s", "TYPE", "-o", "value", dev).CombinedOutput()

	if err != nil {
		if len(out) == 0 {
			return "", nil
		}

		return "", errors.New(string(out))
	}

	return string(out), nil
}

func formatFilesystem(dev string, label string) error {
	out, err := exec.Command("mkfs.ext4", "-L", label, dev).CombinedOutput()

	if err != nil {
		return errors.New(string(out))
	}

	return nil
}

func findDeviceWithTimeout(volId string) (string, error) {
	devicePaths := []string{
		"/dev/disk/by-id/scsi-0QEMU_QEMU_HARDDISK_%s",
		"/dev/disk/by-id/virtio-%.20s",
	}

	for i := 1; i <= 10; i++ {
		time.Sleep(500 * time.Millisecond)
		for _, devicePath := range devicePaths {
			dev := fmt.Sprintf(devicePath, volId)
			if _, err := os.Stat(dev); err != nil {
				if !os.IsNotExist(err) {
					return "", err
				}
			} else {
				return dev, nil
			}
		}
	}

	return "", fmt.Errorf("Block device not found")
}

func isDirectoryPresent(path string) (bool, error) {
	stat, err := os.Stat(path)

	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	} else {
		return stat.IsDir(), nil
	}
}
