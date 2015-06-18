package main

import (
	"flag"
	"fmt"
	pipeline "github.com/soundcloud/pipeline-generator"
	"os"
)

func usage(err error) {
	fmt.Fprintf(os.Stderr, `
USAGE: pipeline {create|delete|update} <pipeline-name>
`)

	if err == nil {
		os.Exit(0)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
}

func main() {
	// uncomment for debug information
	// pipeline.DebugLogger = os.Stderr
	var err error
	flag.Parse()
	args := flag.Args()
	f, err := os.Open("Pipeline.example")
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

	name, err := pipeline.DefaultName()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Please provide 'default-name' setting")
		os.Exit(2)
	}

	switch args[0] {
	case "delete":
		_, err = js.DeletePipeline(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s", err)
		}
	case "update":
		var url string
		url, err = pipeline.UpdatePipeline(name)
		if err == nil {
			fmt.Printf("%s\n", url)
		} else {
			fmt.Fprintf(os.Stderr, "error: %s", err)
		}
	case "create":
		var url string
		url, err = pipeline.CreatePipeline(name)
		if err == nil {
			fmt.Printf("%s\n", url)
		} else {
			fmt.Fprintf(os.Stderr, "error: %s", err)
		}
	default:
		usage(fmt.Errorf("unknown 'dp' subCommand: '%s'", args))
	}
}
