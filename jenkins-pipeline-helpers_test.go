package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func configFileDescriptor(fileName string) (file *os.File) {
	r, err := os.Open(fileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to open file %s: %s", fileName, err.Error())
		os.Exit(1)
	}

	return r
}

func testConfigFile() *ConfigFile {
	if cf == nil {
		fileName := "tests-fixtures/test_config.json"
		var conf ConfigFile
		r := configFileDescriptor(fileName)
		err := json.NewDecoder(r).Decode(&conf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to parse %s: %s", fileName, err.Error())
			os.Exit(1)
		}
		cf = &conf
	}

	return cf
}

func jenkinsConfigFromFile(fileName string) (JenkinsPipeline, error) {
	var pipeline JenkinsPipeline
	r := configFileDescriptor(fileName)

	err := json.NewDecoder(r).Decode(&pipeline)
	if err != nil {
		return JenkinsPipeline{}, fmt.Errorf("unable to parse %s: %s\n", fileName, err.Error())
	}

	return pipeline, nil
}

func jenkinsConfigFromString(jsonString string) (*JenkinsPipeline, error) {
	var pipeline JenkinsPipeline

	r := strings.NewReader(jsonString)

	err := json.NewDecoder(r).Decode(&pipeline)
	if err != nil {
		return nil, err
	}

	return &pipeline, nil
}

func artifactDepArrayEqual(a1 []artifactDep, a2 []artifactDep) bool {
	if len(a1) != len(a2) {
		return false
	}

	for i := 0; i < len(a1); i = i + 1 {
		if a1[i].ProjectNameTempl != a2[i].ProjectNameTempl || a1[i].Artifact != a2[i].Artifact {
			return false
		}
	}

	return true
}
