package main

import (
	"flag"
	"fmt"
	"github.com/soundcloud/pipeline-generator"
	"os"
	"strconv"
)

func usage(err error) {
	fmt.Fprintf(os.Stderr, `
USAGE: pipeline {create|delete|update} <pipeline-name>
       pipeline buildnum <pipeline-name> <nextbuildnum>
`)

	if err == nil {
		os.Exit(0)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
}

func main() {
	var err error
	flag.Parse()
	args := flag.Args()
	if len(args) < 2 {
		usage(fmt.Errorf("pipeline needs at least two arguments"))
	}

	f, err := os.Open("Pipeline")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to open file Pipeline.example: %s", err.Error())
		os.Exit(1)
	}
	defer f.Close()

	pipeline, err := pipeline.NewJenkinsPipeline(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
	js := pipeline.JenkinsServer

	switch args[0] {
	case "delete":
		_, err = js.DeletePipeline(args[1])
	case "update":
		var url string
		url, err = pipeline.UpdatePipeline(args[1])
		if err == nil {
			fmt.Printf("%s\n", url)
		}
	case "create":
		var url string
		url, err = pipeline.CreatePipeline(args[1])
		if err == nil {
			fmt.Printf("%s\n", url)
		}
	case "buildnum":
		if len(args) < 3 {
			usage(fmt.Errorf("pipeline buildnum need two arguments"))
		}
		buildNum, err := strconv.Atoi(args[2])
		if err != nil {
			usage(fmt.Errorf("build number has to be integer: %s", err.Error()))
		}
		err = js.SetBuildNumber(args[1], buildNum)
	default:
		usage(fmt.Errorf("unknown 'dp' subCommand: '%s'", args))
	}
}
