package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type configFile struct {
	Stages   []configStage     `json:"stages"`
	Settings map[string]string `json:"settings"`
}

func (c configFile) isManualStage(stageName string) bool {
	var manualStages []string
	for _, stage := range c.Stages {
		manualStages = append(manualStages, stage.NextManualStages...)
		for _, manualStage := range stage.NextManualStages {
			if stageName == manualStage {
				return true
			}
		}
	}

	return false
}

func (c configFile) nextJobTemplatesForStage(stageNames []string, onlyFirstJob bool) []string {
	nextJobTemplates := []string{}
	for _, stageName := range stageNames {
		jobCnt := 0
		for _, stage := range c.Stages {
			for i, job := range stage.Jobs {
				if stage.Name == stageName {
					if i == 0 || onlyFirstJob == false {
						nextJobTemplates = append(nextJobTemplates, createProjectNameTempl(jobCnt, stageName, job))
					}
				}
				jobCnt += 1 + len(job.SubJobs)
			}
		}
	}

	return nextJobTemplates
}

type configStage struct {
	Name             string      `json:"name"`
	Jobs             []configJob `json:"jobs"`
	NextStages       []string    `json:"next-stages"`
	NextManualStages []string    `json:"next-manual-stages"`
}

type configJob struct {
	Label          string
	Cmd            string
	Artifact       string
	NoClean        bool
	SubJobs        []configJob
	UpstreamJobs   []string
	DownstreamJobs []string
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

func createProjectNameTempl(jobCnt int, stageName string, job configJob) string {
	return fmt.Sprintf("~{{ .PipelineName }}.%02d.%s.%s", jobCnt, stageName, job.taskName())
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
				cj.Cmd = value.(string)
			case map[string]interface{}: // extended job hash
				cj.Label = key
				for jkey, jvalue := range value.(map[string]interface{}) {
					switch jvalueType := jvalue.(type) {
					case string:
						switch jkey {
						case "cmd":
							cj.Cmd = jvalue.(string)
						case "artifact":
							cj.Artifact = jvalue.(string)
						}
					case bool:
						switch jkey {
						case "no-clean":
							cj.NoClean = jvalue.(bool)
						}
					case []interface{}:
						switch jkey {
						case "downstream-jobs":
							for _, downstreamJob := range jvalue.([]interface{}) {
								cj.DownstreamJobs = append(cj.DownstreamJobs, downstreamJob.(string))
							}
						case "upstream-jobs":
							for _, upstreamJob := range jvalue.([]interface{}) {
								cj.UpstreamJobs = append(cj.UpstreamJobs, upstreamJob.(string))
							}
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

			cj.SubJobs = append(cj.SubJobs, job)
		}
	default:
		return fmt.Errorf("unknown type for 'configJob': %#v\n", dataType)
	}

	return nil
}
