package container

import (
	"fmt"
	"github.com/chriskery/tinydocker/pkg/options"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const (
	RUNNING       = "running"
	STOP          = "stopped"
	Exit          = "exited"
	InfoLoc       = "/var/run/tinydocker/"
	InfoLocFormat = InfoLoc + "%s/"
	ConfigName    = "config.json"
	IDLength      = 10
	LogFile       = "container.log"
)

const (
	ImageUrl = "/root"
	RootUrl  = "/root/tinydocker"
)

var (
	LowerDirFormat  = filepath.Join(RootUrl, "%s/lower")
	UpperDirFormat  = filepath.Join(RootUrl, "%s/upper")
	WorkDirFormat   = filepath.Join(RootUrl, "%s/work")
	MergedDirFormat = filepath.Join(RootUrl, "%s/merged")
	OverlayFSFormat = "lowerdir=%s,upperdir=%s,workdir=%s"
)

type Info struct {
	Pid         string   `json:"pid"`         // 容器的init进程在宿主机上的 PID
	Id          string   `json:"id"`          // 容器Id
	Name        string   `json:"name"`        // 容器名
	Command     string   `json:"command"`     // 容器内init运行命令
	CreatedTime string   `json:"createTime"`  // 创建时间
	Status      string   `json:"status"`      // 容器的状态
	Volume      string   `json:"volume"`      // 挂载的数据卷
	PortMapping []string `json:"portmapping"` // 端口映射
}

// NewParentProcess 构建 command 用于启动一个新进程
/*
这里是父进程，也就是当前进程执行的内容。
1.这里的/proc/se1f/exe调用中，/proc/self/ 指的是当前运行进程自己的环境，exec 其实就是自己调用了自己，使用这种方式对创建出来的进程进行初始化
2.后面的args是参数，其中init是传递给本进程的第一个参数，在本例中，其实就是会去调用initCommand去初始化进程的一些环境和资源
3.下面的clone参数就是去fork出来一个新进程，并且使用了namespace隔离新创建的进程和外部环境。
4.如果用户指定了-it参数，就需要把当前进程的输入输出导入到标准输入输出上
*/
func NewParentProcess(flags *options.TinyDockerFlags, image string) (*exec.Cmd, *os.File, error) {
	// 创建匿名管道用于传递参数，将readPipe作为子进程的ExtraFiles，子进程从readPipe中读取参数
	// 父进程中则通过writePipe将参数写入管道
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("new pipe error %v", err)
	}
	initCmd, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return nil, nil, fmt.Errorf("get init process error %v", err)
	}
	cmd := exec.Command(initCmd, "init")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWNET |
			syscall.CLONE_NEWIPC,
	}
	if flags.TTY {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		// 对于后台运行容器，将标准输出重定向到日志文件中，便于后续查询
		dirURL := fmt.Sprintf(InfoLocFormat, flags.ContainerName)
		if err = os.MkdirAll(dirURL, os.ModePerm); err != nil {
			return nil, nil, fmt.Errorf("NewParentProcess mkdir %s error %v", dirURL, err)
		}
		stdLogFilePath := dirURL + LogFile
		stdLogFile, err := os.Create(stdLogFilePath)
		if err != nil {
			return nil, nil, fmt.Errorf("NewParentProcess create file %s error %v", stdLogFilePath, err)
		}
		cmd.Stdout = stdLogFile
	}
	cmd.ExtraFiles = []*os.File{readPipe}
	cmd.Env = append(os.Environ(), flags.Env...)
	cmd.Dir = fmt.Sprintf(MergedDirFormat, flags.ContainerName)
	NewWorkSpace(flags.Volume, image, flags.ContainerName)
	return cmd, writePipe, nil
}
