package container

import (
	"fmt"
	"github.com/chriskery/tinydocker/pkg/constant"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

/*
容器的文件系统相关操作
*/

// NewWorkSpace Create an Overlay2 filesystem as container root workspace
/*
1）创建lower层
2）创建upper、worker层
3）创建merged目录并挂载overlayFS
4）如果有指定volume则挂载volume
*/
func NewWorkSpace(volume, imageName, containerName string) error {
	err := createLower(imageName, containerName)
	if err != nil {
		return fmt.Errorf("createLower err:%v", err)
	}
	err = createUpperWorker(containerName)
	if err != nil {
		return fmt.Errorf("createUpperWorker err:%v", err)
	}
	err = mountOverlayFS(containerName)
	if err != nil {
		return fmt.Errorf("mountOverlayFS err:%v", err)
	}
	if volume != "" {
		volumeURLs := volumeUrlExtract(volume)
		if len(volumeURLs) == 2 && volumeURLs[0] != "" && volumeURLs[1] != "" {
			err = mountVolume(containerName, volumeURLs)
			if err != nil {
				return fmt.Errorf("mountVolume err:%v", err)
			}
		} else {
			log.Infof("volume parameter input is not correct.")
		}
	}
	return nil
}

// DeleteWorkSpace Delete the OverlayFS filesystem while container exit
/*
和创建相反
1）有volume则卸载volume
2）卸载并移除merged目录
3）卸载并移除upper、worker层
*/
func DeleteWorkSpace(volume, containerName string) error {
	log.Infof("volume:%s, containerName:%s", volume, containerName)
	// 如果指定了volume则需要先umount volume
	if volume != "" {
		volumeURLs := volumeUrlExtract(volume)
		length := len(volumeURLs)
		if length == 2 && volumeURLs[0] != "" && volumeURLs[1] != "" {
			err := umountVolume(containerName, volumeURLs)
			if err != nil {
				return errors.Wrap(err, "umountVolume")
			}
		}
	}
	// 接着移除相关文件夹
	err := removeDirs(containerName)
	if err != nil {
		return errors.Wrap(err, "removeDirs")
	}
	// 然后umount整个容器的挂载点
	err = umountOverlayFS(containerName)
	if err != nil {
		return errors.Wrap(err, "umountOverlayFS")
	}
	// 最后把root/containerName目录删除
	root := getRoot(containerName)
	if err = os.RemoveAll(root); err != nil {
		return errors.Wrap(err, "removeRoot")
	}
	return nil
}

// createLower 根据用户输入的镜像为每个容器创建只读层
func createLower(imageName, containerName string) error {
	// 拼接镜像文件所在路径和解压目标位置
	imageUrl := getImage(imageName)
	lower := getLower(containerName)

	// 不存在则创建目录并将将对应镜像解压到目标位置
	if err := os.MkdirAll(lower, constant.Perm0622); err != nil {
		return errors.Wrapf(err, "mkdir %s.", lower)
	}
	if _, err := exec.Command("tar", "-xvf", imageUrl, "-C", lower).CombinedOutput(); err != nil {
		return errors.Wrapf(err, "untar dir %s.", lower)
	}
	return nil
}

// createUpperWorker 创建overlayFS需要的的upper、worker目录
func createUpperWorker(containerName string) error {
	upperURL := getUpper(containerName)
	if err := os.MkdirAll(upperURL, constant.Perm0777); err != nil {
		return errors.Wrapf(err, "mkdir dir %s", upperURL)
	}
	workURL := getWorker(containerName)
	if err := os.MkdirAll(workURL, constant.Perm0777); err != nil {
		return errors.Wrapf(err, "mkdir dir %s", workURL)
	}
	return nil
}

// mountOverlayFS 挂载 overlayFS
func mountOverlayFS(containerName string) error {
	// 创建对应的挂载目录
	mntUrl := fmt.Sprintf(MergedDirFormat, containerName)
	if err := os.MkdirAll(mntUrl, constant.Perm0777); err != nil {
		return errors.Wrapf(err, "mkdir dir %s ", mntUrl)
	}

	var (
		lower  = getLower(containerName)
		upper  = getUpper(containerName)
		worker = getWorker(containerName)
		merged = getMerged(containerName)
	)
	// 完整命令：mount -t overlay overlay -o lowerdir=/root/busybox,upperdir=/root/upper,workdir=/root/work /root/merged
	dirs := getOverlayFSDirs(lower, upper, worker)
	cmd := exec.Command("mount", "-t", "overlay", "overlay", "-o", dirs, merged)
	log.Infof("mountOverlayFS cmd:%s", cmd.String())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	return errors.Wrapf(err, "mount dir %s ", mntUrl)
}

func mountVolume(containerName string, volumeURLs []string) error {
	// 第0个元素为宿主机目录
	parentUrl := volumeURLs[0]
	if err := os.Mkdir(parentUrl, constant.Perm0777); err != nil && !os.IsExist(err) {
		return errors.Wrapf(err, "mkdir parent dir %s", parentUrl)
	}
	// 第1个元素为容器目录
	containerUrl := volumeURLs[1]
	// 拼接并创建对应的容器目录
	mntURL := getMerged(containerName)
	containerVolumeURL := mntURL + "/" + containerUrl
	if err := os.Mkdir(containerVolumeURL, constant.Perm0777); err != nil && !os.IsExist(err) {
		return errors.Wrapf(err, "mkdir container dir %s", containerVolumeURL)
	}
	// 通过bind mount 将宿主机目录挂载到容器目录
	// mount -o bind /hostURL /containerURL
	if _, err := exec.Command("mount", "-o", "bind", parentUrl, containerVolumeURL).CombinedOutput(); err != nil {
		return errors.Wrapf(err, "bind mount %s to %s", parentUrl, containerUrl)
	}
	return nil
}

func umountVolume(containerName string, volumeURLs []string) error {
	mntURL := getMerged(containerName)
	containerUrl := mntURL + volumeURLs[1]
	log.Infof("umount volume url:%s", containerUrl)
	if _, err := exec.Command("umount", containerUrl).CombinedOutput(); err != nil {
		return errors.Wrapf(err, "umount %s", containerUrl)
	}
	return nil
}

func umountOverlayFS(containerName string) error {
	mntURL := getMerged(containerName)
	if _, err := exec.Command("umount", mntURL).CombinedOutput(); err != nil {
		log.Errorf("Umount mountpoint %s failed. %v", mntURL, err)
		return errors.Wrapf(err, "Umount mountpoint %s", mntURL)
	}

	if err := os.RemoveAll(mntURL); err != nil {
		return errors.Wrapf(err, "Remove mountpoint dir %s", mntURL)
	}
	return nil
}

func removeDirs(containerName string) error {
	lower := getLower(containerName)
	upper := getUpper(containerName)
	worker := getWorker(containerName)

	if err := os.RemoveAll(lower); err != nil {
		return errors.Wrapf(err, "remove dir %s", lower)
	}
	if err := os.RemoveAll(upper); err != nil {
		return errors.Wrapf(err, "remove dir %s", upper)
	}
	if err := os.RemoveAll(worker); err != nil {
		return errors.Wrapf(err, "remove dir %s", worker)
	}
	return nil
}

// volumeUrlExtract 通过冒号分割解析volume目录，比如 -v /tmp:/tmp
func volumeUrlExtract(volume string) []string {
	return strings.Split(volume, ":")
}

func getRoot(containerName string) string {
	return filepath.Join(RootUrl, containerName)
}

func getImage(imageName string) string {
	return filepath.Join(ImageUrl, imageName+".tar")
}

func getLower(containerName string) string {
	return fmt.Sprintf(LowerDirFormat, containerName)
}

func getUpper(containerName string) string {
	return fmt.Sprintf(UpperDirFormat, containerName)
}

func getWorker(containerName string) string {
	return fmt.Sprintf(WorkDirFormat, containerName)
}

func getMerged(containerName string) string {
	return fmt.Sprintf(MergedDirFormat, containerName)
}

func getOverlayFSDirs(lower, upper, worker string) string {
	return fmt.Sprintf(OverlayFSFormat, lower, upper, worker)
}

// pathExists returns whether a path exists.
func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
