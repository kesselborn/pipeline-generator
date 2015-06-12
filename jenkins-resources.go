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

var (
	errGitURLMissing        = errors.New("settings/git-url is missing in the pipeline configuration")
	errSettingsMissing      = errors.New("settings section is missing in the pipeline configuration")
	errJenkinsServerMissing = errors.New("settings/jenkins-server is missing in the pipeline configuration")
)

type jenkinsResource interface {
	renderResource(pipelineName string) (io.Reader, error)
	createResource(js JenkinsServer, pipelineName string) error
	projectName(pipelineName string) (string, error)
}

// JenkinsPipeline represents a jenkins pipeline
type JenkinsPipeline struct {
	resources     []jenkinsResource
	defaultName   string
	JenkinsServer JenkinsServer
}

type artifactDep struct {
	ProjectNameTempl string
	Artifact         string
}

type jenkinsJob struct {
	IsInitialJob      bool
	TriggeredManually bool
	CleanWorkspace    bool
	TaskName          string
	ProjectNameTempl  string
	StageName         string
	NextManualJobs    string
	NextJobs          string
}

type jenkinsSingleJob struct {
	jenkinsJob
	Artifact        string
	ArtifactDep     []artifactDep
	IsSubJob        bool
	GitURL          string
	BranchSpecifier string
	Command         string
	SlaveLabel      string
	WorkingDir      string
	UpstreamJobs    string
	Notify          bool
}

type jenkinsMultiJob struct {
	jenkinsJob
	SubJobs []string
}

type jenkinsPipelineView struct {
	Name          string
	jenkinsServer JenkinsServer
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
func (jp JenkinsPipeline) UpdatePipeline(pipelineName string) (string, error) {
	jl, err := jp.JenkinsServer.pipelineJobs(pipelineName)
	if err != nil {
		return "", err
	}
	buildNum, err := jp.JenkinsServer.BuildNumber(pipelineName)
	if err != nil {
		buildNum = 0
	}

	var cur jenkinsServerJob
	for _, resource := range jp.resources {
		switch resource.(type) {
		case jenkinsPipelineView:
		default:
			projectName, err := resource.projectName(pipelineName)
			if err != nil {
				return "", err
			}

			jl, cur, err = jl.remove(projectName)
			if err == nil { // resource already exists: just update
				err = backup(projectName, cur["url"])
				if err != nil {
					return "", err
				}

				src, err := resource.renderResource(pipelineName)
				if err != nil {
					return "", err
				}

				src = debugDumbContent(projectName, src)

				info("update\t%s\n", cur["url"]+"config.xml")

				resp, err := http.Post(cur["url"]+"config.xml", "application/xml", src)
				if err != nil {
					return "", err
				}
				if httpErr := checkResponse(resp); httpErr != nil {
					return "", httpErr
				}
			} else { // create new resource
				info("create\t%s\n", string(jp.JenkinsServer)+"/job/"+projectName)
				if err := resource.createResource(jp.JenkinsServer, pipelineName); err != nil {
					return "", err
				}
			}
		}
	}

	for _, jenkinsJob := range jl.Jobs {
		err := backup(jenkinsJob["name"], jenkinsJob["url"])
		if err != nil {
			return "", err
		}

		info("delete\t%s\n", jenkinsJob["url"])
		if _, err = http.Post(jenkinsJob["url"]+"/doDelete", "application/xml", nil); err != nil {
			return "", err
		}
	}

	err = jp.JenkinsServer.SetBuildNumber(pipelineName, buildNum+1)

	if len(jp.resources) == 1 {
		return jp.JenkinsServer.jobURL(pipelineName), nil
	}

	return jp.JenkinsServer.viewURL(pipelineName), nil

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

// NewJenkinsPipeline returns a JenkinsPipeline by parsing the given configuration
func NewJenkinsPipeline(configuration io.Reader) (JenkinsPipeline, error) {
	var pipeline JenkinsPipeline
	err := json.NewDecoder(configuration).Decode(&pipeline)
	if err != nil {
		return JenkinsPipeline{}, fmt.Errorf("unable to parse pipeline configuration: %s\n", err.Error())
	}

	return pipeline, pipeline.JenkinsServer.Check()
}

func (jmj jenkinsMultiJob) createResource(js JenkinsServer, pipelineName string) error {
	projectName, err := jmj.projectName(pipelineName)
	if err != nil {
		return err
	}

	src, err := jmj.renderResource(pipelineName)
	if err != nil {
		return err
	}
	src = debugDumbContent(projectName, src)

	return js.createJob(projectName, src)
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

func (jj jenkinsSingleJob) createResource(js JenkinsServer, pipelineName string) error {
	projectName, err := jj.projectName(pipelineName)
	if err != nil {
		return err
	}

	src, err := jj.renderResource(pipelineName)
	if err != nil {
		return err
	}
	src = debugDumbContent(projectName, src)

	return js.createJob(projectName, src)
}

func (jpv jenkinsPipelineView) createResource(js JenkinsServer, pipelineName string) error {
	src, err := jpv.renderResource(pipelineName)
	if err != nil {
		return err
	}
	src = debugDumbContent(pipelineName+"_view", src)

	return js.createView(pipelineName, src)
}

func (jj jenkinsJob) projectName(pipelineName string) (string, error) {
	tmpl, err := template.New("jenkinsJob#projectName(" + pipelineName + ")").Parse(jj.ProjectNameTempl)
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

	// 2nd pass: some properties contain {{ .PipelineName }}
	templ, err = template.New(templName + "/2").Parse(firstRender.String())
	if err != nil {
		return nil, err
	}

	var secondRender bytes.Buffer
	err = templ.Execute(&secondRender, struct{ PipelineName string }{pipelineName})

	return &secondRender, err

}

func (jpv jenkinsPipelineView) projectName(pipelineName string) (string, error) {
	tmpl, err := template.New("jenkinsPipelineView#projectName(" + pipelineName + ")").Parse(jpv.Name)
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

func newJenkinsMultiJob(conf configFile, job configJob, setup string, stage configStage, nextJobsTemplates string, nextManualJobsTemplate string, stageJobCnt int, jobCnt int, notify bool) (jenkinsMultiJob, []jenkinsSingleJob) {
	projectNameTempl := []string{createProjectNameTempl(jobCnt, stage.Name, job)}
	var subJobs []jenkinsSingleJob
	var subJobsTemplates []string

	for _, subJob := range job.SubJobs {
		jobCnt++
		jenkinsJob := newJenkinsJob(conf, subJob, setup, stage, "", "", stageJobCnt, jobCnt, notify)
		jenkinsJob.IsSubJob = true
		jenkinsJob.TaskName = "---- " + jenkinsJob.TaskName // indent sub jobs
		subJobs = append(subJobs, jenkinsJob)
		subJobsTemplates = append(subJobsTemplates, jenkinsJob.ProjectNameTempl)
	}

	jenkinsMultiJob := jenkinsMultiJob{
		jenkinsJob: jenkinsJob{
			IsInitialJob:     jobCnt == 0,
			TaskName:         "parallel execution",
			StageName:        stage.Name,
			ProjectNameTempl: strings.Join(projectNameTempl, "_"),
			NextJobs:         nextJobsTemplates,
			NextManualJobs:   nextManualJobsTemplate,
		},
		SubJobs: subJobsTemplates,
	}

	return jenkinsMultiJob, subJobs
}

func newJenkinsJob(conf configFile, job configJob, setup string, stage configStage, nextJobsTemplates string, nextManualJobsTemplate string, stageJobCnt int, jobCnt int, notify bool) jenkinsSingleJob {
	projectNameTempl := createProjectNameTempl(jobCnt, stage.Name, job)

	gitBranch, gitBranchPresent := conf.Settings["git-branch"]
	gitURL, _ := conf.Settings["git-url"]

	command := setup + "# job\n" + job.Cmd
	jenkinsJob := jenkinsSingleJob{
		jenkinsJob: jenkinsJob{
			IsInitialJob:     jobCnt == 0,
			TaskName:         job.Label,
			StageName:        stage.Name,
			ProjectNameTempl: projectNameTempl,
			NextJobs:         nextJobsTemplates,
			CleanWorkspace:   !job.NoClean,
			NextManualJobs:   nextManualJobsTemplate,
		},
		Notify:       notify,
		Artifact:     job.Artifact,
		GitURL:       gitURL.(string),
		Command:      command,
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
	var conf configFile
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
				nextJobsTemplates = strings.Join(append(conf.nextJobTemplatesForStage(stage.NextStages, false), job.DownstreamJobs...), ",")
				nextManualJobsTemplate = strings.Join(conf.nextJobTemplatesForStage(stage.NextStages, true), ",")
			} else {
				nextJob := stage.Jobs[stageJobCnt+1]
				if nextJob.TriggeredManually {
					nextJobsTemplates = strings.Join(job.DownstreamJobs, ",")
					nextManualJobsTemplate = createProjectNameTempl(jobCnt+len(job.SubJobs)+1, stage.Name, stage.Jobs[stageJobCnt+1])
				} else {
					nextJobsTemplates = strings.Join(append([]string{createProjectNameTempl(jobCnt+len(job.SubJobs)+1, stage.Name, stage.Jobs[stageJobCnt+1])}, job.DownstreamJobs...), ",")
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

				if jobCnt == 0 { // first job gets a nice name + polls git repo
					jenkinsJob.ProjectNameTempl = "{{ .PipelineName }}"
					jenkinsJob.WorkingDir = workingDir
				}

				pipeline.resources = append(pipeline.resources, jenkinsJob)
				jobCnt++
			}
		}
	}

	// set artifact dependencies
	for i, res := range pipeline.resources {
		switch res.(type) {
		case jenkinsSingleJob:
			current := res.(jenkinsSingleJob)
			if current.Artifact != "" {
				if len(pipeline.resources) > i+1 {
					setArtifactDep(&pipeline, current, i+1, false)
				}
			}
		}
	}

	// only create pipeline view if there are more than one job
	if len(pipeline.resources) > 1 {
		pipeline.resources = append(pipeline.resources, jenkinsPipelineView{"{{ .PipelineName }}", pipeline.JenkinsServer})
	}

	*jp = pipeline

	return err
}

func setArtifactDep(jp *JenkinsPipeline, current jenkinsSingleJob, index int, differentMultiJob bool) {
	ad := artifactDep{current.ProjectNameTempl, current.Artifact}
	switch jp.resources[index].(type) {
	case jenkinsSingleJob:
		nextJob := jp.resources[index].(jenkinsSingleJob)
		if current.IsSubJob == false || differentMultiJob {
			nextJob.ArtifactDep = append(nextJob.ArtifactDep, ad)
			jp.resources[index] = nextJob
		} else { // sub job artifacts are fetched in the next non-sub-job (sub jobs == parallel jobs)
			found := false
			for i := index; len(jp.resources) > i+1 && found == false; i++ {
				switch jp.resources[i].(type) {
				case jenkinsSingleJob:
					if jp.resources[i].(jenkinsSingleJob).IsSubJob != true {
						setArtifactDep(jp, current, i, true)
						found = true
					}
				case jenkinsMultiJob:
					setArtifactDep(jp, current, i, true)
					found = true
				}
			}
		}
	case jenkinsMultiJob: // set deps on subjobs, not on the multijob
		for i := range jp.resources[index].(jenkinsMultiJob).SubJobs {
			setArtifactDep(jp, current, i+index+1, true)
		}
	}
}
