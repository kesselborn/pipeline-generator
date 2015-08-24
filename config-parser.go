package pipeline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	errManualTriggerInMultiJob    = errors.New("a job that is part of a multi job can not set to be triggered manually")
	errNextManualStagesDeprecated = errors.New("next-manual-stages setting is deprecated, please adjust your pipeline configuration according to http://git.io/xYoE")
)

type ConfigFile struct {
	Stages   []configStage          `json:"stages"`
	Settings map[string]interface{} `json:"settings"`
}

// deprecated property
type deprManStages []string

type configStage struct {
	Jobs             []configJob   `json:"jobs"`
	Name             string        `json:"name"`
	NextStages       []string      `json:"next-stages"`
	NextManualStages deprManStages `json:"next-manual-stages"`
}

type configJob struct {
	Artifacts         []string
	Cmd               string
	DownstreamJobs    []string
	Label             string
	NoClean           bool
	SubJobs           []configJob
	TestReports       string
	TriggeredManually bool
	UpstreamJobs      []string
}

func (c ConfigFile) nextJobs(nextStages []string) []string {
	return c.nextJobTemplatesForStage(nextStages, false)
}

func (c ConfigFile) nextManualJobs(nextStages []string) []string {
	return c.nextJobTemplatesForStage(nextStages, true)
}

// return the job names of all jobs that follow the stages in stageNames
func (c ConfigFile) nextJobTemplatesForStage(nextStages []string, manualOnlyMode bool) []string {
	nextJobNames := []string{}
	for _, nextStage := range nextStages {
		jobCnt := 0 // all job names of a pipeline include a counter
		for _, stage := range c.Stages {
			if stage.Name == nextStage { // only include given stages
				firstJob := stage.Jobs[0]
				if (manualOnlyMode && firstJob.TriggeredManually) ||
					(!manualOnlyMode && !firstJob.TriggeredManually) {
					nextJobNames = append(nextJobNames, jobNameTemplate(jobCnt, nextStage, firstJob))
				}
			}
			for _, job := range stage.Jobs {
				jobCnt += 1 + len(job.SubJobs)
			}
		}
	}

	return nextJobNames
}

func (cj configJob) taskName() string {
	if cj.isMultiJob() {
		taskName := []string{"multi_"}
		for _, subJob := range cj.SubJobs {
			taskName = append(taskName, subJob.taskName())
		}
		return strings.Join(taskName, "_")
	}

	return cj.Label
}

func (cj configJob) isMultiJob() bool {
	return len(cj.SubJobs) > 0
}

func jobNameTemplate(jobCnt int, stageName string, job configJob) string {
	return fmt.Sprintf("~{{ .PipelineName }}.%02d.%s.%s", jobCnt, stageName, job.taskName())
}

func escape(s string) string {
	return strings.Replace(s, "&", "&amp;", -1)
}

// fail on deprecated attribute
func (_ *deprManStages) UnmarshalJSON(jsonString []byte) error {
	return errNextManualStagesDeprecated
}

// UnmarshalJSON correctly creates a configJob which can represent one job or a multijob
func (cj *configJob) UnmarshalJSON(jsonString []byte) error {
	var data interface{}
	r := bytes.NewReader(jsonString)
	err := json.NewDecoder(r).Decode(&data)
	if err != nil {
		return err
	}

	switch dataType := data.(type) {
	case map[string]interface{}: // normal job
		for key, value := range data.(map[string]interface{}) {
			switch valueType := value.(type) {
			case string: // normal job
				cj.Label = key
				cj.Cmd = escape(value.(string))
			case map[string]interface{}: // extended job hash
				cj.Label = key
				for jkey, jvalue := range value.(map[string]interface{}) {
					switch jvalueType := jvalue.(type) {
					case string:
						switch jkey {
						case "cmd":
							cj.Cmd = escape(jvalue.(string))
						case "test-reports":
							cj.TestReports = jvalue.(string)
						case "artifact":
							artifacts := strings.Split(jvalue.(string), ",")
							return fmt.Errorf("\ndeprecated attribute syntax:\n\n\"artifact\": \"%s\"\n\nuse\n\n\"artifacts\": [\"%s\"]\n\ninstead\n", strings.Join(artifacts, ","), strings.Join(artifacts, `","`))
						default:
							return fmt.Errorf("unknown string options passed in: %s", jkey)
						}
					case bool:
						switch jkey {
						case "no-clean":
							cj.NoClean = jvalue.(bool)
						case "manual":
							cj.TriggeredManually = jvalue.(bool)
						default:
							return fmt.Errorf("unknown bool options passed in: %s", jkey)
						}
					case []interface{}:
						switch jkey {
						case "downstream-jobs":
							for _, downstreamJob := range jvalue.([]interface{}) {
								cj.DownstreamJobs = append(cj.DownstreamJobs, downstreamJob.(string))
							}
						case "artifacts":
							for _, artifact := range jvalue.([]interface{}) {
								cj.Artifacts = append(cj.Artifacts, artifact.(string))
							}
						case "upstream-jobs":
							for _, upstreamJob := range jvalue.([]interface{}) {
								cj.UpstreamJobs = append(cj.UpstreamJobs, upstreamJob.(string))
							}
						default:
							return fmt.Errorf("unknown array option passed in: %s", jkey)
						}
					default:
						return fmt.Errorf("job hash must only contain string or bool values, got %#v for key %s", jvalueType, jkey)
					}
				}
				if cj.Cmd == "" {
					return fmt.Errorf("job hash for job %s needs to contain key 'cmd', got %#v", key, value)
				}
			default:
				return fmt.Errorf("value for job '%s' must be a string, got a %#v\n", key, valueType)
			}
		}
	case []interface{}: // parallel jobs
		for _, item := range data.([]interface{}) {
			var job configJob

			subJobData, err := json.Marshal(item)
			if err != nil {
				return err
			}
			err = job.UnmarshalJSON(subJobData)
			if err != nil {
				return err
			}

			if job.TriggeredManually {
				return errManualTriggerInMultiJob
			}

			cj.SubJobs = append(cj.SubJobs, job)
		}
	default:
		return fmt.Errorf("unknown type for 'configJob': %#v\n", dataType)
	}

	return nil
}
