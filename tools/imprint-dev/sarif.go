package main

import (
	"encoding/json"
	"os"
)

// SARIF 2.1.0, only as much of it as this tool writes.

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool        sarifTool         `json:"tool"`
	Invocations []sarifInvocation `json:"invocations"`
	Results     []sarifResult     `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name  string      `json:"name"`
	Rules []sarifRule `json:"rules"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifRule struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	ShortDescription sarifText `json:"shortDescription"`
}

type sarifInvocation struct {
	ExecutionSuccessful        bool                `json:"executionSuccessful"`
	ToolExecutionNotifications []sarifNotification `json:"toolExecutionNotifications,omitempty"`
}

type sarifNotification struct {
	Level   string    `json:"level"`
	Message sarifText `json:"message"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	RuleIndex int             `json:"ruleIndex"`
	Kind      string          `json:"kind"`
	Level     string          `json:"level"`
	Message   sarifText       `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

func buildSARIF(r report) sarifLog {
	driver := sarifDriver{Name: "imprint-dev"}
	index := map[string]int{}
	for i, c := range checks {
		index[c.ID] = i
		driver.Rules = append(driver.Rules, sarifRule{
			ID:               c.ID,
			Name:             "check " + c.Letter,
			ShortDescription: sarifText{Text: c.Title},
		})
	}
	inv := sarifInvocation{ExecutionSuccessful: true}
	if r.Fatal != nil {
		inv.ExecutionSuccessful = false
		inv.ToolExecutionNotifications = append(inv.ToolExecutionNotifications,
			sarifNotification{Level: "error", Message: sarifText{Text: r.Fatal.Error()}})
	}
	results := []sarifResult{}
	for _, o := range r.Outcomes {
		if o.Err != nil {
			inv.ExecutionSuccessful = false
			inv.ToolExecutionNotifications = append(inv.ToolExecutionNotifications,
				sarifNotification{Level: "error", Message: sarifText{Text: o.Check.ID + " could not run: " + o.Err.Error()}})
		}
		for _, f := range o.Result.Findings {
			res := sarifResult{
				RuleID:    f.Rule,
				RuleIndex: index[f.Rule],
				Kind:      "fail",
				Level:     "error",
				Message:   sarifText{Text: f.Message},
			}
			if f.Kind == notCheckable {
				// SARIF allows a level other than "none" only on kind "fail".
				res.Kind, res.Level = "notApplicable", "none"
			}
			if f.Path != "" {
				loc := sarifLocation{PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: f.Path},
				}}
				if f.Line > 0 {
					loc.PhysicalLocation.Region = &sarifRegion{StartLine: f.Line}
				}
				res.Locations = []sarifLocation{loc}
			}
			results = append(results, res)
		}
	}
	return sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool:        sarifTool{Driver: driver},
			Invocations: []sarifInvocation{inv},
			Results:     results,
		}},
	}
}

func writeSARIF(path string, r report) error {
	data, err := json.MarshalIndent(buildSARIF(r), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
