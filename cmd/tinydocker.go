package main

import (
	"github.com/chriskery/tinydocker/cmd/app"
	"github.com/sirupsen/logrus"
	"k8s.io/component-base/cli"
	"os"
)

func init() {
	logrus.SetReportCaller(true)
}

func main() {
	command := app.NewTinyDockerCommand()
	code := cli.Run(command)
	os.Exit(code)
}
