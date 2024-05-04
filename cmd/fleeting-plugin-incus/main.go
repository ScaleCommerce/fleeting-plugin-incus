package main

import (
	"flag"
	"fmt"
	"os"

	fleetingincus "fleeting-plugin-incus"

	"gitlab.com/gitlab-org/fleeting/fleeting/plugin"
)

const version string = "v0.1.0"

func main() {
	showVersion := flag.Bool("version", false, "Show version information and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	instanceGroup := fleetingincus.InstanceGroup{}

	plugin.Serve(&instanceGroup)
}
