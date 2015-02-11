package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
)

func configFileDescriptor(fileName string) (file *os.File) {
	r, err := os.Open(fileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to open file %s: %s", fileName, err.Error())
		os.Exit(1)
	}

	return r
}

func testConfigFile() *configFile {
	if cf == nil {
		fileName := "tests-fixtures/test_config.json"
		var conf configFile
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

type expectation struct {
	description string
	expected    interface{}
	actual      interface{}
}

func ExpectationErrorf(file string, line int, ok bool, t *testing.T, format string, a ...interface{}) {
	if ok {
		// Truncate file name at last file name separator.
		if index := strings.LastIndex(file, "/"); index >= 0 {
			file = file[index+1:]
		} else if index = strings.LastIndex(file, "\\"); index >= 0 {
			file = file[index+1:]
		}
	} else {
		file = "???"
		line = 1
	}
	decoratedFormat := fmt.Sprintf("\t\033[1;31;48m%s:%d: %s\033[m\n", file, line, format)

	fmt.Fprintf(os.Stderr, decoratedFormat, a...)
	t.Fail()
}

func (test expectation) Run(t *testing.T) {
	_, file, line, ok := runtime.Caller(1)
	test.runTest(file, line, ok, t)
}

func (test expectation) runTest(file string, line int, ok bool, t *testing.T) {
	if testing.Verbose() {
		fmt.Printf("\t- check: %s\n", test.description)
	}

	switch test.actual.(type) {
	case []string:
		if !stringArrayEqual(test.actual.([]string), test.expected.([]string)) {
			ExpectationErrorf(file, line, ok, t, "'%s' failed!\nExpected: '%#v'\ngot     : '%#v'", test.description, test.expected, test.actual)
		}
	case []artifactDep:
		if !artifactDepArrayEqual(test.actual.([]artifactDep), test.expected.([]artifactDep)) {
			ExpectationErrorf(file, line, ok, t, "'%s' failed!\nExpected: '%#v'\ngot     : '%#v'", test.description, test.expected, test.actual)
		}
	case map[string]string:
		actual := test.actual.(map[string]string)
		expected := test.expected.(map[string]string)
		if len(actual) != len(expected) {
			ExpectationErrorf(file, line, ok, t, "'%s' failed!\nExpected: '%#v'\ngot     : '%#v'", test.description, test.expected, test.actual)
		}
		for key, value := range actual {
			if expected[key] != value {
				ExpectationErrorf(file, line, ok, t, "'%s' failed!\nExpected: '%#v'\ngot     : '%#v'", test.description, test.expected, test.actual)
			}
		}
	default:
		if test.actual != test.expected {
			ExpectationErrorf(file, line, ok, t, "'%s' failed!\nexpected: '%#v'\ngot     : '%#v'", test.description, test.expected, test.actual)
		}
	}
}

type expectations []expectation

func (expectations expectations) Run(t *testing.T) {
	_, file, line, ok := runtime.Caller(1)
	for _, test := range expectations {
		test.runTest(file, line, ok, t)
	}
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

func stringArrayEqual(a1 []string, a2 []string) bool {
	if len(a1) != len(a2) {
		return false
	}

	for i := 0; i < len(a1); i = i + 1 {
		if a1[i] != a2[i] {
			return false
		}
	}

	return true
}
