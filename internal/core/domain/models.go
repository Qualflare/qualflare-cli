package domain

import "time"

// Launch represents the complete test launch
type Launch struct {
	Project   string `json:"project"`
	Framework string `json:"framework"`
	Platform  string `json:"platform"`
	OS        string `json:"os"`

	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`

	Environment string `json:"environment"`
	Milestone   int64  `json:"milestone,omitempty"`

	Properties map[string]string `json:"properties,omitempty"`
	Suites     []Suite           `json:"suites"`
}

type Metadata struct {
	Version   string `json:"version"`
	Timestamp string `json:"timestamp"`
}

// Suite represents a collection of test cases
type Suite struct {
	Name string `json:"name"`

	Total   int `json:"total"`
	Success int `json:"success"`
	Failure int `json:"failure"`
	Skipped int `json:"skipped"`

	Status   Status        `json:"status"`
	Duration time.Duration `json:"duration"`

	Properties map[string]string `json:"properties,omitempty"`
	Cases      []Case            `json:"cases"`
}

// Case represents a single suite case
type Case struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	ClassName string        `json:"className"`
	Status    Status        `json:"status"`
	Duration  time.Duration `json:"duration"`
	Error     string        `json:"error,omitempty"`
}

// Status represents the status of a test
type Status string

const (
	StatusSuccess Status = "success"
	StatusFailure Status = "failure"
	StatusSkipped Status = "skipped"
)

// Framework represents supported test frameworks
type Framework string

const (
	FrameworkJUnit    Framework = "junit"
	FrameworkPython   Framework = "python"
	FrameworkGolang   Framework = "golang"
	FrameworkCucumber Framework = "cucumber"
)
