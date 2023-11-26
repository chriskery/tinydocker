package network

import (
	"encoding/json"
	"fmt"
	"github.com/chriskery/tinydocker/pkg/constant"
	"github.com/chriskery/tinydocker/pkg/container"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/pkg/errors"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"

	"github.com/sirupsen/logrus"
)

var (
	defaultNetworkPath = "/var/run/mydocker/network/network/"
	drivers            = map[string]Driver{}
	networks           = map[string]*Network{}
)

type Endpoint struct {
	ID          string           `json:"id"`
	Device      netlink.Veth     `json:"dev"`
	IPAddress   net.IP           `json:"ip"`
	MacAddress  net.HardwareAddr `json:"mac"`
	Network     *Network
	PortMapping []string
}

type Network struct {
	Name    string     // 网络名
	IPRange *net.IPNet // 地址段
	Driver  string     // 网络驱动名
}

type Driver interface {
	Name() string
	Create(subnet string, name string) (*Network, error)
	Delete(network Network) error
	Connect(network *Network, endpoint *Endpoint) error
	Disconnect(network Network, endpoint *Endpoint) error
}

func (nw *Network) dump(dumpPath string) error {
	// 检查保存的目录是否存在，不存在则创建
	if _, err := os.Stat(dumpPath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err = os.MkdirAll(dumpPath, constant.Perm0644); err != nil {
			return errors.Wrapf(err, "create network dump path %s failed", dumpPath)
		}
	}
	// 保存的文件名是网络的名字
	nwPath := path.Join(dumpPath, nw.Name)
	// 打开保存的文件用于写入,后面打开的模式参数分别是存在内容则清空、只写入、不存在则创建
	nwFile, err := os.OpenFile(nwPath, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, constant.Perm0644)
	if err != nil {
		return errors.Wrapf(err, "open file %s failed", dumpPath)
	}
	defer nwFile.Close()

	nwJson, err := json.Marshal(nw)
	if err != nil {
		return errors.Wrapf(err, "Marshal %v failed", nw)
	}

	_, err = nwFile.Write(nwJson)
	return errors.Wrapf(err, "write %s failed", nwJson)
}

func (nw *Network) remove(dumpPath string) error {
	// 检查网络对应的配置文件状态，如果文件己经不存在就直接返回
	fullPath := path.Join(dumpPath, nw.Name)
	if _, err := os.Stat(fullPath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	// 否则删除这个网络对应的配置文件
	return os.Remove(fullPath)
}

func (nw *Network) load(dumpPath string) error {
	// 打开配置文件
	nwConfigFile, err := os.Open(dumpPath)
	if err != nil {
		return err
	}
	defer nwConfigFile.Close()
	// 从配置文件中读取网络 配置 json 符串
	nwJson := make([]byte, 2000)
	n, err := nwConfigFile.Read(nwJson)
	if err != nil {
		return err
	}

	err = json.Unmarshal(nwJson[:n], nw)
	return errors.Wrapf(err, "unmarshal %s failed", nwJson[:n])
}

func Init() error {
	// 加载网络驱动
	var bridgeDriver = BridgeNetworkDriver{}
	drivers[bridgeDriver.Name()] = &bridgeDriver
	// 文件不存在则创建
	if _, err := os.Stat(defaultNetworkPath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err = os.MkdirAll(defaultNetworkPath, constant.Perm0644); err != nil {
			return err
		}
	}
	// 检查网络配置目录中的所有文件,并执行第二个参数中的函数指针去处理目录下的每一个文件
	err := filepath.Walk(defaultNetworkPath, func(nwPath string, info os.FileInfo, err error) error {
		// 如果是目录则跳过
		if info.IsDir() {
			return nil
		}
		// if strings.HasSuffix(nwPath, "/") {
		// 	return nil
		// }
		//  加载文件名作为网络名
		_, nwName := path.Split(nwPath)
		nw := &Network{
			Name: nwName,
		}
		// 调用前面介绍的 Network.load 方法加载网络的配置信息
		if err = nw.load(nwPath); err != nil {
			logrus.Errorf("error load network: %s", err)
		}
		// 将网络的配置信息加入到 networks 字典中
		networks[nwName] = nw
		return nil
	})
	return err
}

func CreateNetwork(driver, subnet, name string) error {
	// 将网段的字符串转换成net. IPNet的对象
	_, cidr, _ := net.ParseCIDR(subnet)
	// 通过IPAM分配网关IP，获取到网段中第一个IP作为网关的IP
	ip, err := ipAllocator.Allocate(cidr)
	if err != nil {
		return err
	}
	cidr.IP = ip
	// 调用指定的网络驱动创建网络，这里的 drivers 字典是各个网络驱动的实例字典 通过调用网络驱动
	// Create 方法创建网络，后面会以 Bridge 驱动为例介绍它的实现
	nw, err := drivers[driver].Create(cidr.String(), name)
	if err != nil {
		return err
	}
	// 保存网络信息，将网络的信息保存在文件系统中，以便查询和在网络上连接网络端点
	return nw.dump(defaultNetworkPath)
}

func ListNetwork() {
	// 通过tabwriter库把信息打印到屏幕上
	w := tabwriter.NewWriter(os.Stdout, 12, 1, 3, ' ', 0)
	fmt.Fprint(w, "NAME\tIpRange\tDriver\n")
	for _, nw := range networks {
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			nw.Name,
			nw.IPRange.String(),
			nw.Driver,
		)
	}
	if err := w.Flush(); err != nil {
		logrus.Errorf("Flush error %v", err)
		return
	}
}

func DeleteNetwork(networkName string) error {
	// 网络不存在直接返回一个error
	nw, ok := networks[networkName]
	if !ok {
		return fmt.Errorf("no Such Network: %s", networkName)
	}
	// 调用IPAM的实例ipAllocator释放网络网关的IP
	if err := ipAllocator.Release(nw.IPRange, &nw.IPRange.IP); err != nil {
		return errors.Wrap(err, "remove Network gateway ip failed")
	}
	// 调用网络驱动删除网络创建的设备与配置 后面会以 Bridge 驱动删除网络为例子介绍如何实现网络驱动删除网络
	if err := drivers[nw.Driver].Delete(*nw); err != nil {
		return errors.Wrap(err, "remove Network DriverError failed")
	}
	// 最后从网络的配直目录中删除该网络对应的配置文件
	return nw.remove(defaultNetworkPath)
}

// enterContainerNetNS 将容器的网络端点加入到容器的网络空间中
// 并锁定当前程序所执行的线程，使当前线程进入到容器的网络空间
// 返回值是一个函数指针，执行这个返回函数才会退出容器的网络空间，回归到宿主机的网络空间
func enterContainerNetNS(enLink *netlink.Link, info *container.Info) func() {
	// 找到容器的Net Namespace
	// /proc/[pid]/ns/net 打开这个文件的文件描述符就可以来操作Net Namespace
	// 而ContainerInfo中的PID,即容器在宿主机上映射的进程ID
	// 它对应的/proc/[pid]/ns/net就是容器内部的Net Namespace
	f, err := os.OpenFile(fmt.Sprintf("/proc/%s/ns/net", info.Pid), os.O_RDONLY, 0)
	if err != nil {
		logrus.Errorf("error get container net namespace, %v", err)
	}

	nsFD := f.Fd()
	// 锁定当前程序所执行的线程，如果不锁定操作系统线程的话
	// Go语言的goroutine可能会被调度到别的线程上去
	// 就不能保证一直在所需要的网络空间中了
	// 所以先调用runtime.LockOSThread()锁定当前程序执行的线程
	runtime.LockOSThread()

	// 修改网络端点Veth的另外一端，将其移动到容器的Net Namespace 中
	if err = netlink.LinkSetNsFd(*enLink, int(nsFD)); err != nil {
		logrus.Errorf("error set link netns , %v", err)
	}

	// 获取当前的网络namespace
	origns, err := netns.Get()
	if err != nil {
		logrus.Errorf("error get current netns, %v", err)
	}

	// 调用 netns.Set方法，将当前进程加入容器的Net Namespace
	if err = netns.Set(netns.NsHandle(nsFD)); err != nil {
		logrus.Errorf("error set netns, %v", err)
	}
	// 返回之前Net Namespace的函数
	// 在容器的网络空间中执行完容器配置之后调用此函数就可以将程序恢复到原生的Net Namespace
	return func() {
		// 恢复到上面获取到的之前的 Net Namespace
		netns.Set(origns)
		origns.Close()
		// 取消对当附程序的线程锁定
		runtime.UnlockOSThread()
		f.Close()
	}
}

// configEndpointIpAddressAndRoute 配置容器网络端点的地址和路由
func configEndpointIpAddressAndRoute(ep *Endpoint, info *container.Info) error {
	// 根据名字找到对应Veth设备
	peerLink, err := netlink.LinkByName(ep.Device.PeerName)
	if err != nil {
		return fmt.Errorf("fail config endpoint: %v", err)
	}
	// 将容器的网络端点加入到容器的网络空间中
	// 并使这个函数下面的操作都在这个网络空间中进行
	// 执行完函数后，恢复为默认的网络空间，具体实现下面再做介绍

	defer enterContainerNetNS(&peerLink, info)()
	// 获取到容器的IP地址及网段，用于配置容器内部接口地址
	// 比如容器IP是192.168.1.2， 而网络的网段是192.168.1.0/24
	// 那么这里产出的IP字符串就是192.168.1.2/24，用于容器内Veth端点配置

	interfaceIP := *ep.Network.IPRange
	interfaceIP.IP = ep.IPAddress
	// 设置容器内Veth端点的IP
	if err = setInterfaceIP(ep.Device.PeerName, interfaceIP.String()); err != nil {
		return fmt.Errorf("%v,%s", ep.Network, err)
	}
	// 启动容器内的Veth端点
	if err = setInterfaceUP(ep.Device.PeerName); err != nil {
		return err
	}
	// Net Namespace 中默认本地地址 127 的勺。”网卡是关闭状态的
	// 启动它以保证容器访问自己的请求
	if err = setInterfaceUP("lo"); err != nil {
		return err
	}
	// 设置容器内的外部请求都通过容器内的Veth端点访问
	// 0.0.0.0/0的网段，表示所有的IP地址段
	_, cidr, _ := net.ParseCIDR("0.0.0.0/0")
	// 构建要添加的路由数据，包括网络设备、网关IP及目的网段
	// 相当于route add -net 0.0.0.0/0 gw (Bridge网桥地址) dev （容器内的Veth端点设备)

	defaultRoute := &netlink.Route{
		LinkIndex: peerLink.Attrs().Index,
		Gw:        ep.Network.IPRange.IP,
		Dst:       cidr,
	}
	// 调用netlink的RouteAdd,添加路由到容器的网络空间
	// RouteAdd 函数相当于route add 命令
	if err = netlink.RouteAdd(defaultRoute); err != nil {
		return err
	}

	return nil
}

// configPortMapping 配置端口映射
func configPortMapping(ep *Endpoint) error {
	var err error
	// 遍历容器端口映射列表
	for _, pm := range ep.PortMapping {
		// 分割成宿主机的端口和容器的端口
		portMapping := strings.Split(pm, ":")
		if len(portMapping) != 2 {
			logrus.Errorf("port mapping format error, %v", pm)
			continue
		}
		// 由于iptables没有Go语言版本的实现，所以采用exec.Command的方式直接调用命令配置
		// 在iptables的PREROUTING中添加DNAT规则
		// 将宿主机的端口请求转发到容器的地址和端口上
		iptablesCmd := fmt.Sprintf("-t nat -A PREROUTING -p tcp -m tcp --dport %s -j DNAT --to-destination %s:%s",
			portMapping[0], ep.IPAddress.String(), portMapping[1])
		cmd := exec.Command("iptables", strings.Split(iptablesCmd, " ")...)
		logrus.Infoln("配置端口映射 cmd:", cmd.String())
		// 执行iptables命令,添加端口映射转发规则
		output, err := cmd.Output()
		if err != nil {
			logrus.Errorf("iptables Output, %v", output)
			continue
		}
	}
	return err
}

// Connect 连接容器到之前创建的网络 mydocker run -net testnet -p 8080:80 xxxx
func Connect(networkName string, info *container.Info) error {
	// 从networks字典中取到容器连接的网络的信息，networks字典中保存了当前己经创建的网络
	network, ok := networks[networkName]
	if !ok {
		return fmt.Errorf("no Such Network: %s", networkName)
	}

	// 分配容器IP地址
	ip, err := ipAllocator.Allocate(network.IPRange)
	if err != nil {
		return errors.Wrapf(err, "allocate ip")
	}

	// 创建网络端点
	ep := &Endpoint{
		ID:          fmt.Sprintf("%s-%s", info.Id, networkName),
		IPAddress:   ip,
		Network:     network,
		PortMapping: info.PortMapping,
	}
	// 调用网络驱动挂载和配置网络端点
	if err = drivers[network.Driver].Connect(network, ep); err != nil {
		return err
	}
	// 到容器的namespace配置容器网络设备IP地址
	if err = configEndpointIpAddressAndRoute(ep, info); err != nil {
		return err
	}
	// 配置端口映射信息，例如 mydocker run -p 8080:80
	return configPortMapping(ep)
}

func Disconnect(networkName string, info *container.Info) error {
	return nil
}
