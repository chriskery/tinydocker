package cmds

import (
	"encoding/json"
	"fmt"
	"github.com/chriskery/tinydocker/pkg/cgroups"
	"github.com/chriskery/tinydocker/pkg/constant"
	"github.com/chriskery/tinydocker/pkg/container"
	"github.com/chriskery/tinydocker/pkg/network"
	"github.com/chriskery/tinydocker/pkg/options"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/uuid"
	"os"
	"strconv"
	"strings"
	"time"
)

func NewTinyDockerRunCommand() *cobra.Command {
	tinyDockerFlags := options.NewTinyDockerFlags()
	allCmd := &cobra.Command{
		Use:   "run",
		Short: "Create and run a new container from an image",
		RunE: func(cmd *cobra.Command, args []string) error {
			// get image name
			image := args[0]
			if len(args) > 1 {
				args = args[1:]
			}

			return runRun(tinyDockerFlags, image, args)
			//return runRunInMain(tinyDockerFlags, image, args)
		},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("missing container command")
			}
			return nil
		},
	}
	// keep cleanFlagSet separate, so Cobra doesn't pollute it with the global flags
	tinyDockerFlags.AddFlags(allCmd.Flags())
	return allCmd
}

func runRun(flags *options.TinyDockerFlags, image string, args []string) error {
	containerID := getRandomContainerID()
	if flags.ContainerName == "" {
		flags.ContainerName = containerID
	}
	var parent, writePipe, err = container.NewParentProcess(flags, image)
	if err != nil {
		return err
	}
	if parent == nil {
		return errors.New("New parent process error")
	}
	if err = parent.Start(); err != nil {
		return fmt.Errorf("run parent.Stnewart err:%v", err)
	}
	// record container info
	if err = recordContainerInfo(parent.Process.Pid, args, containerID, flags); err != nil {
		return fmt.Errorf("record container info error %v", err)
	}

	// 创建cgroup manager, 并通过调用set和apply设置资源限制并使限制在容器上生效
	cgroupManager := cgroups.NewCgroupManager("tinydocker-cgroup")
	defer cgroupManager.Destroy()
	_ = cgroupManager.Set(&flags.ResourceConfig)
	_ = cgroupManager.Apply(parent.Process.Pid, &flags.ResourceConfig)

	if flags.Net != "" {
		// config container network
		err = network.Init()
		if err != nil {
			return err
		}
		containerInfo := &container.Info{
			Id:          containerID,
			Pid:         strconv.Itoa(parent.Process.Pid),
			Name:        flags.ContainerName,
			PortMapping: flags.PortMapping,
		}
		if err = network.Connect(flags.ContainerName, containerInfo); err != nil {
			return fmt.Errorf("error Connect Network %v", err)
		}
	}

	// 在子进程创建后才能通过管道来发送参数
	sendInitCommand(args, writePipe)
	if flags.TTY { // 如果是tty，那么父进程等待
		if err = parent.Wait(); err != nil {
			return err
		}
		deleteContainerInfo(flags.ContainerName)
		_ = container.DeleteWorkSpace(flags.Volume, flags.ContainerName)
	}
	return nil
}

func getRandomContainerID() string {
	containerID := string(uuid.NewUUID())
	return strings.ReplaceAll(containerID, "-", "")
}

func deleteContainerInfo(containerName string) {
	dirURL := fmt.Sprintf(container.InfoLocFormat, containerName)
	if err := os.RemoveAll(dirURL); err != nil {
		logrus.Errorf("remove dir %s error %v", dirURL, err)
	}
}

// sendInitCommand 通过writePipe将指令发送给子进程
func sendInitCommand(cmdArray []string, writePipe *os.File) {
	command := strings.Join(cmdArray, " ")
	_, _ = writePipe.WriteString(command)
	_ = writePipe.Close()
}
func recordContainerInfo(containerPID int, cmdArray []string, containerID string, flags *options.TinyDockerFlags) error {
	// 以当前时间作为容器创建时间
	createTime := time.Now().Format("2006-01-02 15:04:05")
	command := strings.Join(cmdArray, "")
	containerInfo := &container.Info{
		Id:          containerID,
		Pid:         strconv.Itoa(containerPID),
		Command:     command,
		CreatedTime: createTime,
		Status:      container.RUNNING,
		Name:        flags.Name,
		Volume:      flags.Volume,
	}

	jsonBytes, err := json.Marshal(containerInfo)
	if err != nil {
		return fmt.Errorf("record container info error %v", err)
	}
	jsonStr := string(jsonBytes)
	// 拼接出存储容器信息文件的路径，如果目录不存在则级联创建
	dirUrl := fmt.Sprintf(container.InfoLocFormat, flags.ContainerName)
	if err = os.MkdirAll(dirUrl, constant.Perm0622); err != nil {
		return fmt.Errorf("mkdir error %s error %v", dirUrl, err)
	}
	// 将容器信息写入文件
	fileName := dirUrl + "/" + container.ConfigName
	file, err := os.Create(fileName)
	defer file.Close()
	if err != nil {
		return fmt.Errorf("create file %s error %v", fileName, err)
	}
	if _, err = file.WriteString(jsonStr); err != nil {
		return fmt.Errorf("file write string error %v", err)
	}

	return nil
}
