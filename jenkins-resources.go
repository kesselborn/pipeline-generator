package pipeline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"text/template"
)

const (
	pipelineNamePlaceholder = "{{ .PipelineName }}"
)

var (
	errGitURLMissing        = errors.New("settings/git-url is missing in the pipeline configuration")
	errSettingsMissing      = errors.New("settings section is missing in the pipeline configuration")
	errJenkinsServerMissing = errors.New("settings/jenkins-server is missing in the pipeline configuration")
)

type jenkinsResource interface {
	renderResource(pipelineName string) (io.Reader, error)
	createResource(js JenkinsServer, pipelineName string) error
	name(pipelineName string) (string, error)
}

// JenkinsPipeline represents a jenkins pipeline
type JenkinsPipeline struct {
	resources     []jenkinsResource
	defaultName   string
	JenkinsServer JenkinsServer
}

// artifact that is created in a job
type artifact struct {
	JobName  string
	Artifact string
}

type jenkinsJob struct {
	IsInitialJob      bool
	TriggeredManually bool
	CleanWorkspace    bool
	TaskName          string
	JobName           string
	StageName         string
	NextManualJobs    string
	NextJobs          string
}

type jenkinsSingleJob struct {
	jenkinsJob

	Artifact        string
	ArtifactDep     []artifact
	BranchSpecifier string
	Command         string
	GitURL          string
	IsSubJob        bool
	Notify          bool
	SlaveLabel      string
	TestReports     string
	UpstreamJobs    string
	WorkingDir      string
}

type jenkinsMultiJob struct {
	jenkinsJob
	SubJobs []string
}

type jenkinsPipelineView struct {
	Name          string
	jenkinsServer JenkinsServer
}

// NewJenkinsPipeline returns a JenkinsPipeline by parsing the given configuration
func NewJenkinsPipeline(configuration io.Reader) (JenkinsPipeline, error) {
	var pipeline JenkinsPipeline
	err := json.NewDecoder(configuration).Decode(&pipeline)
	if err != nil {
		return JenkinsPipeline{}, fmt.Errorf("unable to parse pipeline configuration: %s\n", err.Error())
	}

	return pipeline, pipeline.JenkinsServer.Check()
}

// DefaultName returns a default name which can be set in the configuration file
func (jp JenkinsPipeline) DefaultName() (string, error) {
	if jp.defaultName == "" {
		return "", fmt.Errorf("no default name set in configuration file")
	}
	return jp.defaultName, nil
}

// UpdatePipeline updates the existing pipeline on JenkinsServer keeping as much state
// as possible but using the updated config
func (jp JenkinsPipeline) UpdatePipeline(name string) (string, error) {
	jobs, err := jp.JenkinsServer.pipelineJobs(name)
	if err != nil {
		return "", err
	}

	buildNum, err := jp.JenkinsServer.BuildNumber(name)
	if err != nil {
		buildNum = 0
	}

	var cur jenkinsServerJob
	for _, resource := range jp.resources {
		switch resource.(type) {
		case jenkinsPipelineView:
		default:
			resourceName, err := resource.name(name)
			if err != nil {
				return "", err
			}

			jobs, cur, err = jobs.remove(resourceName)
			if err == nil { // resource already exists: just update
				err = backup(resourceName, cur["url"])
				if err != nil {
					return "", err
				}

				src, err := resource.renderResource(name)
				if err != nil {
					return "", err
				}

				src = debugDumbContent(resourceName, src)

				info("update\t%s\n", cur["url"]+"config.xml")

				return "", jp.JenkinsServer.updateJob(cur["url"], src)
			} else { // create new resource
				info("create\t%s\n", string(jp.JenkinsServer)+"/job/"+resourceName)
				if err := resource.createResource(jp.JenkinsServer, name); err != nil {
					return "", err
				}
			}
		}
	}

	// the remaining jobs to not exist anymore due to a changed pipeline config: let's delete them
	for _, jenkinsJob := range jobs.Jobs {
		err := backup(jenkinsJob["name"], jenkinsJob["url"])
		if err != nil {
			return "", err
		}

		info("delete\t%s\n", jenkinsJob["url"])
		if _, err = http.Post(jenkinsJob["url"]+"/doDelete", "application/xml", nil); err != nil {
			return "", err
		}
	}

	err = jp.JenkinsServer.SetBuildNumber(name, buildNum+1)

	if len(jp.resources) == 1 {
		return jp.JenkinsServer.jobURL(name), nil
	}

	return jp.JenkinsServer.viewURL(name), nil

}

// CreatePipeline creates the pipeline on JenkinsServer
func (jp JenkinsPipeline) CreatePipeline(pipelineName string) (string, error) {
	if l, err := jp.JenkinsServer.pipelineJobs(pipelineName); len(l.Jobs) > 0 || err != nil {
		if err != nil {
			return "", err
		}

		jobURLs := []string{}
		for _, job := range l.Jobs {
			jobURLs = append(jobURLs, "\n\t"+job["url"])
		}

		return "", fmt.Errorf("there is already a pipeline with this name. Conflicting URLs: %s\n", jobURLs)
	}

	for _, resource := range jp.resources {
		if err := resource.createResource(jp.JenkinsServer, pipelineName); err != nil {
			return "", err
		}
	}

	if len(jp.resources) == 1 {
		return jp.JenkinsServer.jobURL(pipelineName), nil
	}

	return jp.JenkinsServer.viewURL(pipelineName), nil
}

func (jj jenkinsJob) createResource(js JenkinsServer, pipelineName string) error {
	return fmt.Errorf("can't create resource for type jenkinsJob")
}

func (jmj jenkinsMultiJob) createResource(js JenkinsServer, pipelineName string) error {
	resourceName, err := jmj.name(pipelineName)
	if err != nil {
		return err
	}

	src, err := jmj.renderResource(pipelineName)
	if err != nil {
		return err
	}
	src = debugDumbContent(resourceName, src)

	return js.createJob(resourceName, src)
}

func (jj jenkinsSingleJob) createResource(js JenkinsServer, pipelineName string) error {
	resourceName, err := jj.name(pipelineName)
	if err != nil {
		return err
	}

	src, err := jj.renderResource(pipelineName)
	if err != nil {
		return err
	}
	src = debugDumbContent(resourceName, src)

	return js.createJob(resourceName, src)
}

func (jpv jenkinsPipelineView) createResource(js JenkinsServer, pipelineName string) error {
	src, err := jpv.renderResource(pipelineName)
	if err != nil {
		return err
	}
	src = debugDumbContent(pipelineName+"_view", src)

	return js.createView(pipelineName, src)
}

func (jj jenkinsJob) name(pipelineName string) (string, error) {
	tmpl, err := template.New("jenkinsJob#resourceName(" + pipelineName + ")").Parse(jj.JobName)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	err = tmpl.Execute(&b, struct{ PipelineName string }{pipelineName})
	if err != nil {
		return "", err
	}

	return b.String(), err
}

func (jj jenkinsJob) renderResource(pipelineName string) (io.Reader, error) {
	return nil, fmt.Errorf("can't render jenkinsJob resource")
}

func (jj jenkinsSingleJob) renderResource(pipelineName string) (io.Reader, error) {
	return render("templates/jenkins/normal-job.xml", jj, pipelineName)
}

func (jmj jenkinsMultiJob) renderResource(pipelineName string) (io.Reader, error) {
	return render("templates/jenkins/multi-job.xml", jmj, pipelineName)
}

func (jpv jenkinsPipelineView) renderResource(pipelineName string) (io.Reader, error) {
	return render("templates/jenkins/pipeline.xml", jpv, pipelineName)
}

func render(templName string, templ1Data interface{}, pipelineName string) (io.Reader, error) {
	templateSrc, err := Asset(templName)
	if err != nil {
		return nil, err
	}

	// 1st pass
	templ, err := template.New(templName + "/1").Parse(string(templateSrc))
	if err != nil {
		return nil, err
	}

	var firstRender bytes.Buffer
	err = templ.Execute(&firstRender, templ1Data)
	if err != nil {
		return nil, err
	}

	// 2nd pass: some properties contain {{ .PipelineName }} as the name can be given as a parameter
	templ, err = template.New(templName + "/2").Parse(firstRender.String())
	if err != nil {
		return nil, fmt.Errorf("error rendering template: %s\n\ntemplate content: %s", err, firstRender.String())
	}

	var secondRender bytes.Buffer
	err = templ.Execute(&secondRender, struct{ PipelineName string }{pipelineName})

	return &secondRender, err

}

func (jpv jenkinsPipelineView) name(pipelineName string) (string, error) {
	tmpl, err := template.New("jenkinsPipelineView#resourceName(" + pipelineName + ")").Parse(jpv.Name)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	err = tmpl.Execute(&b, struct{ PipelineName string }{pipelineName})
	if err != nil {
		return "", err
	}

	return b.String(), nil
}

func newJenkinsMultiJob(conf ConfigFile, job configJob, setup string, stage configStage, nextJobsTemplates string, nextManualJobsTemplate string, stageJobCnt int, jobCnt int, notify bool) (jenkinsMultiJob, []jenkinsSingleJob) {
	resourceName := []string{jobNameTemplate(jobCnt, stage.Name, job)}
	var subJobs []jenkinsSingleJob
	var subJobsTemplates []string

	for _, subJob := range job.SubJobs {
		jobCnt++
		jenkinsJob := newJenkinsJob(conf, subJob, setup, stage, "", "", stageJobCnt, jobCnt, notify)
		jenkinsJob.IsSubJob = true
		jenkinsJob.TaskName = "---- " + jenkinsJob.TaskName // indent sub jobs
		subJobs = append(subJobs, jenkinsJob)
		subJobsTemplates = append(subJobsTemplates, jenkinsJob.JobName)
	}

	jenkinsMultiJob := jenkinsMultiJob{
		jenkinsJob: jenkinsJob{
			IsInitialJob:   jobCnt == 0,
			TaskName:       "parallel execution",
			StageName:      stage.Name,
			JobName:        strings.Join(resourceName, "_"),
			NextJobs:       nextJobsTemplates,
			NextManualJobs: nextManualJobsTemplate,
		},
		SubJobs: subJobsTemplates,
	}

	return jenkinsMultiJob, subJobs
}

func newJenkinsJob(conf ConfigFile, job configJob, setup string, stage configStage, nextJobsTemplates string, nextManualJobsTemplate string, stageJobCnt int, jobCnt int, notify bool) jenkinsSingleJob {
	resourceName := jobNameTemplate(jobCnt, stage.Name, job)

	gitBranch, gitBranchPresent := conf.Settings["git-branch"]
	gitURL, _ := conf.Settings["git-url"]

	command := setup + "# job\n" + job.Cmd
	jenkinsJob := jenkinsSingleJob{
		jenkinsJob: jenkinsJob{
			IsInitialJob:   jobCnt == 0,
			TaskName:       job.Label,
			StageName:      stage.Name,
			JobName:        resourceName,
			NextJobs:       nextJobsTemplates,
			CleanWorkspace: !job.NoClean,
			NextManualJobs: nextManualJobsTemplate,
		},
		Notify:       notify,
		Artifact:     strings.Join(job.Artifacts, ","),
		GitURL:       gitURL.(string),
		Command:      command,
		TestReports:  job.TestReports,
		UpstreamJobs: strings.Join(job.UpstreamJobs, ","),
	}

	if slaveLabel, slaveLabelPresent := conf.Settings["slave-label"]; slaveLabelPresent {
		jenkinsJob.SlaveLabel = slaveLabel.(string)
	}

	if gitBranchPresent {
		jenkinsJob.BranchSpecifier = gitBranch.(string)
	} else {
		jenkinsJob.BranchSpecifier = "master"
	}

	if job.TriggeredManually {
		jenkinsJob.TaskName = "|>| " + jenkinsJob.TaskName
		jenkinsJob.TriggeredManually = true
	}

	return jenkinsJob
}

// UnmarshalJSON gets called implicitly when passing a JenkinsPipeline variable to a json parser
//
// This should only be used if the configuration json is embedded in another json file -- otherwise
// use NewJenkinsPipeline
func (jp *JenkinsPipeline) UnmarshalJSON(jsonString []byte) error {
	var conf ConfigFile
	var pipeline JenkinsPipeline
	err := json.NewDecoder(bytes.NewReader(jsonString)).Decode(&conf)

	if err != nil {
		return err
	}

	_js, jenkinsServerPresent := conf.Settings["jenkins-server"]
	_gitURL, gitURLPresent := conf.Settings["git-url"]
	switch {
	case len(conf.Settings) == 0:
		return errSettingsMissing
	case jenkinsServerPresent != true || _js.(string) == "":
		return errJenkinsServerMissing
	case gitURLPresent != true || _gitURL.(string) == "":
		return errGitURLMissing
	}
	js := _js.(string)

	notify := true
	if _silent, silentPresent := conf.Settings["silent"]; silentPresent {
		notify = !_silent.(bool)
	}

	pipeline.JenkinsServer = JenkinsServer(js)

	var setup string
	if _setup, present := conf.Settings["job-setup"]; present == true {
		setup = "\n# job setup\n" + _setup.(string) + "\n\n"
	}

	if defaultName, present := conf.Settings["default-name"]; present == true {
		pipeline.defaultName = defaultName.(string)
	}

	var workingDir string
	if _workindDir, present := conf.Settings["working-dir"]; present == true {
		workingDir = _workindDir.(string) + "/.*"
		setup = "\n# change to working dir:\ncd " + _workindDir.(string) + "\n\n" + setup
	}

	jobCnt := 0
	for _, stage := range conf.Stages {
		for stageJobCnt, job := range stage.Jobs {
			var nextJobsTemplates string
			var nextManualJobsTemplate string

			if stageJobCnt == len(stage.Jobs)-1 { // last job in stage uses explict next-jobs
				nextJobsTemplates = strings.Join(append(conf.nextJobs(stage.NextStages), job.DownstreamJobs...), ",")
				nextManualJobsTemplate = strings.Join(conf.nextManualJobs(stage.NextStages), ",")
			} else {
				nextJob := stage.Jobs[stageJobCnt+1]
				if nextJob.TriggeredManually {
					nextJobsTemplates = strings.Join(job.DownstreamJobs, ",")
					nextManualJobsTemplate = jobNameTemplate(jobCnt+len(job.SubJobs)+1, stage.Name, stage.Jobs[stageJobCnt+1])
				} else {
					nextJobsTemplates = strings.Join(append([]string{jobNameTemplate(jobCnt+len(job.SubJobs)+1, stage.Name, stage.Jobs[stageJobCnt+1])}, job.DownstreamJobs...), ",")
				}
			}

			if job.isMultiJob() == true {
				multijob, subJobs := newJenkinsMultiJob(conf, job, setup, stage, nextJobsTemplates, nextManualJobsTemplate, stageJobCnt, jobCnt, notify)

				pipeline.resources = append(pipeline.resources, multijob)
				for _, subJob := range subJobs {
					pipeline.resources = append(pipeline.resources, subJob)
				}

				jobCnt += 1 + len(subJobs)
			} else {
				jenkinsJob := newJenkinsJob(conf, job, setup, stage, nextJobsTemplates, nextManualJobsTemplate, stageJobCnt, jobCnt, notify)

				if jobCnt == 0 { // first job gets a nice name
					jenkinsJob.JobName = pipelineNamePlaceholder
					jenkinsJob.WorkingDir = workingDir
				}

				pipeline.resources = append(pipeline.resources, jenkinsJob)
				jobCnt++
			}
		}
	}

	setupArtifactCopyCmds(&pipeline)
	// set artifact dependencies
	//for i, res := range pipeline.resources {
	//	switch res.(type) {
	//	case jenkinsSingleJob:
	//		current := res.(jenkinsSingleJob)
	//		if current.Artifact != "" {
	//			if len(pipeline.resources) > i+1 {
	//				setArtifactDep(&pipeline, current, i+1, false)
	//			}
	//		}
	//	}
	//}

	// only create pipeline view if there are more than one job
	if len(pipeline.resources) > 1 {
		pipeline.resources = append(pipeline.resources, jenkinsPipelineView{pipelineNamePlaceholder, pipeline.JenkinsServer})
	}

	*jp = pipeline

	return err
}

// if a job creates artifacts, make them available for the next job as well. If the next job is a multi job,
// make the artifacts available to all sub-jobs. A job after a multijob should get all artifacts from all
// sub jobs
func setupArtifactCopyCmds(jp *JenkinsPipeline) error {
	resCnt := len(jp.resources) - 1

	for resCnt > 0 {
		to := jp.resources[resCnt] // artifacts should be copied to this job

		for _, from := range jp.resources {
			switch from.(type) {
			case jenkinsSingleJob: // only jenkinsSingleJob produce artifacts
				fromJob := from.(jenkinsSingleJob)
				if res, _ := jp.wantsCopyArtifacts(from, to); res == true {
					switch to.(type) {
					case jenkinsSingleJob:
						toJob := to.(jenkinsSingleJob)
						fmt.Printf("GGGGGGGGGGGGGGGGGGGGGGGG S %#v -> %#v\n", fromJob.JobName, toJob.JobName)
						toJob.ArtifactDep = append(toJob.ArtifactDep, artifact{fromJob.JobName, fromJob.Artifact})
						jp.resources[resCnt] = toJob
					case jenkinsMultiJob:
						fmt.Printf("GGGGGGGGGGGGGGGGGGGGGGGG M %#v -> %#v\n", fromJob.JobName, to.(jenkinsMultiJob).JobName)
						for _, toSubJobName := range to.(jenkinsMultiJob).SubJobs {
							toSubJobIndex, err := jp.indexOf(toSubJobName)
							if err != nil {
								fmt.Printf("PPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPP")
								return err
							}

							toSubJob := jp.resources[toSubJobIndex].(jenkinsSingleJob)
							fmt.Printf("CCCCCCCCCCCCCCCCC %s -> %s\n", toSubJob.JobName, fromJob.JobName)
							toSubJob.ArtifactDep = append(toSubJob.ArtifactDep, artifact{fromJob.JobName, fromJob.Artifact})
							jp.resources[toSubJobIndex] = toSubJob
						}
					}
				}
			}
		}
		resCnt--
	}

	return nil
}

func (jp JenkinsPipeline) indexOf(resName string) (int, error) {
	for i, res := range jp.resources {
		curName, _ := res.name(pipelineNamePlaceholder)
		if curName == resName {
			return i, nil
		}
	}

	return -1, fmt.Errorf("no resource named '%s' found\n", resName)
}

func (jp JenkinsPipeline) wantsCopyArtifacts(from, to jenkinsResource) (bool, error) {
	switch from.(type) {
	case jenkinsSingleJob:
		fromJob := from.(jenkinsSingleJob)
		toName, _ := to.name(pipelineNamePlaceholder)

		if fromJob.IsSubJob {
			fromName, _ := from.name(pipelineNamePlaceholder)
			fromJobIndex, err := jp.indexOf(fromName)
			if err != nil {
				return false, err
			}
			//fmt.Printf("WWWWWWWWWWWWWWWWWWWWWW %s -> %s\n", fromName, toName)
			return jp.wantsCopyArtifacts(jp.resources[fromJobIndex-1], to)
		}

		allNextJobs := append(strings.Split(fromJob.NextJobs, ","), strings.Split(fromJob.NextManualJobs, ",")...)

		//fmt.Fprintf(os.Stderr, "TTTTTTTTTTTTTTTTTTTTTTTT %#v / %s\n", allNextJobs, toName)
		for _, job := range allNextJobs {
			if job == toName {
				fmt.Fprintf(os.Stderr, "EEEEEEEEEEEEEEEEEEEEEEEEEEE %s == %s\n", job, toName)
				return true, nil
			}
		}
	case jenkinsMultiJob:
		fromJob := from.(jenkinsMultiJob)
		toName, _ := to.name(pipelineNamePlaceholder)
		allNextJobs := append(strings.Split(fromJob.NextJobs, ","), strings.Split(fromJob.NextManualJobs, ",")...)

		//fmt.Fprintf(os.Stderr, "TTTTTTTTTTTTTTTTTTTTTTTT %#v / %s\n", allNextJobs, toName)
		for _, job := range allNextJobs {
			if job == toName {
				fmt.Fprintf(os.Stderr, "EEEEEEEEEEEEEEEEEEEEEEEEEEE M %s == %s\n", fromJob.JobName, toName)
				return true, nil
			}
		}
	}

	return false, nil
}

func debugDumbContent(name string, content io.Reader) io.Reader {
	if debugMode() {
		f, err := ioutil.TempFile("", "__"+name+".xml__")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating xml dump for debugging: %s\n", err.Error())
			os.Exit(1)
		}
		defer f.Close()
		contentInBytes, err := ioutil.ReadAll(content)
		_, err = f.Write(contentInBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating xml dump for debugging: %s\n", err.Error())
			os.Exit(2)
		}
		content = strings.NewReader(string(contentInBytes))
		debug("dumped config.xml for '%s' to %s\n", name, f.Name())
	}

	return content
}
