package options

import (
	"github.com/chriskery/tinydocker/pkg/cgroups/subsystems"
	"github.com/spf13/pflag"
)

type TinyDockerFlags struct {
	TTY             bool
	DetachContainer bool

	MemoryLimit   string
	CpuLimit      string
	Volume        string
	Name          string
	ContainerName string
	Env           []string
	Net           string
	PortMapping   []string

	subsystems.ResourceConfig
}

func NewTinyDockerFlags() *TinyDockerFlags {
	return &TinyDockerFlags{}
}

// AddFlags adds flags for a specific KubeletFlags to the specified FlagSet
func (f *TinyDockerFlags) AddFlags(mainfs *pflag.FlagSet) {
	fs := pflag.NewFlagSet("", pflag.ExitOnError)
	defer func() {
		// Unhide deprecated flags. We want deprecated flags to show in Kubelet help.
		// We have some hidden flags, but we might as well unhide these when they are deprecated,
		// as silently deprecating and removing (even hidden) things is unkind to people who use them.
		fs.VisitAll(func(f *pflag.Flag) {
			if len(f.Deprecated) > 0 {
				f.Hidden = false
			}
		})
		mainfs.AddFlagSet(fs)
	}()

	fs.BoolVar(&f.TTY, "it", false, "")
	fs.BoolVar(&f.DetachContainer, "d", false, "")

	fs.StringVar(&f.MemoryLimit, "mem", "", "")
	fs.StringVar(&f.CpuLimit, "cpu", "", "")
	fs.StringVar(&f.Volume, "volume", "", "")
	fs.StringVar(&f.ContainerName, "name", "", "")
	fs.StringVar(&f.Net, "net", "", "")

	fs.StringArrayVar(&f.PortMapping, "p", []string{}, "")
	fs.StringArrayVar(&f.Env, "env", []string{}, "")
}
