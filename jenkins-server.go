package pipeline

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strconv"
)

// JenkinsServer provides interaction with a jenkins server
type JenkinsServer string

type jenkinsPlugin struct {
	ShortName string `json:"shortName"`
	Version   string `json:"version"`
}

type jenkinsPlugins struct {
	List []jenkinsPlugin `json:"plugins"`
}

type jenkinsServerJob map[string]string

type jenkinsJobList struct {
	Jobs []jenkinsServerJob
}

func (js JenkinsServer) createJob(jobName string, content io.Reader) error {
	_, err := http.Post(string(js)+"/createItem?name="+jobName, "application/xml", content)

	return err
}

func (js JenkinsServer) createView(viewName string, content io.Reader) error {
	_, err := http.Post(string(js)+"/createView?name="+viewName, "application/xml", content)

	return err
}

func (js JenkinsServer) viewURL(viewName string) string {
	return string(js) + "/view/" + viewName
}

func (js JenkinsServer) jobList() (jenkinsJobList, error) {
	var jobList jenkinsJobList

	resp, err := http.Get(string(js) + "/api/json")
	if err != nil {
		return jobList, err
	}

	err = json.NewDecoder(resp.Body).Decode(&jobList)
	if err != nil {
		return jobList, fmt.Errorf("error decoding %#v: %s\n", resp.Body, err.Error())
	}

	return jobList, nil
}

func (jl jenkinsJobList) remove(name string) (jenkinsJobList, jenkinsServerJob, error) {
	jobs := jl.Jobs
	for i, job := range jobs {
		if job["name"] == name {
			if i == len(jobs)-1 {
				return jenkinsJobList{jobs[:i]}, job, nil
			}
			return jenkinsJobList{append(jobs[:i], jobs[i+1:]...)}, job, nil
		}
	}
	return jl, jenkinsServerJob{}, fmt.Errorf("no job named '%s' found", name)
}

func (js JenkinsServer) pipelineJobs(name string) (jenkinsJobList, error) {
	l, err := js.jobList()
	if err != nil {
		return jenkinsJobList{}, err
	}

	jobs := []jenkinsServerJob{}
	subJobRegexp := regexp.MustCompile(`~` + name + `\.[0-9][0-9]\.`)

	for _, job := range l.Jobs {
		if job["name"] == name || subJobRegexp.MatchString(job["name"]) {
			jobs = append(jobs, job)
		}
	}

	return jenkinsJobList{jobs}, nil
}

func backup(name, url string) error {
	f, err := ioutil.TempFile("", "__"+name+".xml__")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error backing up %s -- chickening out", url)
		os.Exit(1)
	}
	defer f.Close()
	fmt.Printf("backup\t%s\t%s\n", url, f.Name())

	resp, err := http.Get(url + "config.xml")
	if err != nil {
		return err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	fmt.Fprintf(f, "%s", body)

	return nil
}

func (jps jenkinsPlugins) installed(plugins []string) error {
	for _, plugin := range plugins {
		found := false
		for _, installedPlugin := range jps.List {
			if installedPlugin.ShortName == plugin {
				found = true
				break
			}
		}
		if found == false {
			return fmt.Errorf("jenkins server not setup correctly: required plugin '%s' not installed", plugin)
		}
	}

	return nil
}

// Check returns nil when all necessary plugins are installed, an error otherwise
func (js JenkinsServer) Check() error {
	resp, err := http.Get(string(js) + "/pluginManager/api/json?pretty=true&depth=1")
	if err != nil {
		return fmt.Errorf("error checking jenkins server: %s", err.Error())
	}

	var plugins jenkinsPlugins
	err = json.NewDecoder(resp.Body).Decode(&plugins)
	if err != nil {
		return err
	}

	return plugins.installed([]string{
		"build-pipeline-plugin",
		"copyartifact",
		"delivery-pipeline-plugin",
		"git",
		"jenkins-multijob-plugin",
		"next-build-number",
		"parameterized-trigger",
	})
}

// DeletePipeline deletes all jobs and views of the named pipeline
func (js JenkinsServer) DeletePipeline(name string) (int, error) {
	l, err := js.pipelineJobs(name)
	if err != nil {
		return 0, err
	}

	buildNum, err := js.BuildNumber(name)
	if err != nil {
		return 0, err
	}

	for _, jenkinsJob := range l.Jobs {
		err := backup(jenkinsJob["name"], jenkinsJob["url"])
		if err != nil {
			return 0, err
		}

		if _, err = http.Post(jenkinsJob["url"]+"/doDelete", "application/xml", nil); err != nil {
			return 0, err
		}
	}

	err = backup("pipeline-view-"+name, string(js)+"/view/"+name+"/")
	if err != nil {
		return 0, err
	}

	_, err = http.Post(string(js)+"/view/"+name+"/doDelete", "application/xml", nil)

	fmt.Printf("lastbuildnum\t%d\n", buildNum)

	return buildNum, err
}

// BuildNumber returns the latest build number for a named job
func (js JenkinsServer) BuildNumber(job string) (int, error) {
	url := string(js) + "/job/" + job + "/lastBuild/buildNumber"
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode == 404 {
		return 0, nil
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(string(body))
}

// SetBuildNumber sets the next build number for named job to given build number
func (js JenkinsServer) SetBuildNumber(job string, buildNumber int) error {
	_, err := http.Post(string(js)+"/job/"+job+"/nextbuildnumber/submit?nextBuildNumber="+strconv.Itoa(buildNumber), "application/xml", nil)

	return err
}
