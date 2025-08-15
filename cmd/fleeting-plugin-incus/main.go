package main

import (
	"flag"
	"fmt"
	"os"

	fleetingincus "fleeting-plugin-incus"

	"gitlab.com/gitlab-org/fleeting/fleeting/plugin"
)

func main() {
	showVersion := flag.Bool("version", false, "Show version information and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("fleeting-plugin-incus %s\n", fleetingincus.Version)
		fmt.Printf("Build: %s\n", fleetingincus.BuildInfo)
		fmt.Printf("Date: %s\n", fleetingincus.BuildDate)
		fmt.Printf("Commit: %s\n", fleetingincus.GitCommit)
		os.Exit(0)
	}

	instanceGroup := fleetingincus.InstanceGroup{}

	plugin.Serve(&instanceGroup)
}
