package container

import (
	"os/exec"

	"github.com/pkg/errors"
)

func Commit(containerName, imageName string) error {
	mntURL := getMerged(containerName)
	imageTar := getImage(imageName)
	if _, err := exec.Command("tar", "-czf", imageTar, "-C", mntURL, ".").CombinedOutput(); err != nil {
		return errors.Wrapf(err, "tar folder %s", mntURL)
	}
	return nil
}
