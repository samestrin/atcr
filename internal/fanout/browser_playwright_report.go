package fanout

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Attachment names the spec gives testInfo.attach for the data the parser
// reads. Playwright itself names its failure screenshots "screenshot".
const (
	playwrightScreenshotAttachment = "screenshot"
	playwrightConsoleAttachment    = "console"
	playwrightNetworkAttachment    = "network"
)

// playwrightReport is the part of Playwright's JSON reporter output the parser
// reads.
type playwrightReport struct {
	Suites []playwrightSuite `json:"suites"`
}

type playwrightSuite struct {
	Title  string            `json:"title"`
	File   string            `json:"file"`
	Specs  []playwrightSpec  `json:"specs"`
	Suites []playwrightSuite `json:"suites"`
}

type playwrightSpec struct {
	Title string           `json:"title"`
	File  string           `json:"file"`
	Tests []playwrightTest `json:"tests"`
}

type playwrightTest struct {
	Results []playwrightTestResult `json:"results"`
}

type playwrightTestResult struct {
	Status      string                 `json:"status"`
	Errors      []playwrightError      `json:"errors"`
	Attachments []playwrightAttachment `json:"attachments"`
}

type playwrightError struct {
	Message string `json:"message"`
}

// playwrightAttachment's Body is base64 in the report; encoding/json decodes
// it into bytes.
type playwrightAttachment struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Path        string `json:"path"`
	Body        []byte `json:"body"`
}

// playwrightConsoleItem is one element of the JSON array a spec attaches as
// "console". Level is read from "type" (Playwright's ConsoleMessage.type()) or
// "level".
type playwrightConsoleItem struct {
	Type     string `json:"type"`
	Level    string `json:"level"`
	Text     string `json:"text"`
	Location string `json:"location"`
}

// playwrightNetworkItem is one element of the JSON array a spec attaches as
// "network". Error is read from "error" or "failure".
type playwrightNetworkItem struct {
	URL     string `json:"url"`
	Method  string `json:"method"`
	Status  int    `json:"status"`
	Error   string `json:"error"`
	Failure string `json:"failure"`
}

// ParsePlaywrightReport turns the output of `playwright test --reporter=json`
// into a BrowserResult: the failed tests, the screenshot attachments, and the
// console and network data the spec attached with testInfo.attach under the
// names "console" and "network". Backend, ExitCode and TimedOut are the
// caller's to fill. Input that is not a Playwright report, or an attachment
// that is not the documented JSON, is an error rather than an empty result.
func ParsePlaywrightReport(data []byte) (BrowserResult, error) {
	var report playwrightReport
	if err := json.Unmarshal(data, &report); err != nil {
		return BrowserResult{}, fmt.Errorf("fanout: parse playwright report: %w", err)
	}
	if report.Suites == nil {
		return BrowserResult{}, errors.New("fanout: parse playwright report: no \"suites\" key, not a Playwright JSON report")
	}
	var res BrowserResult
	for _, suite := range report.Suites {
		if err := collectPlaywrightSuite(&res, suite, nil); err != nil {
			return BrowserResult{}, err
		}
	}
	return res, nil
}

func collectPlaywrightSuite(res *BrowserResult, suite playwrightSuite, parents []string) error {
	titles := parents
	// A file-level suite is titled with its file name; keep it out of test titles.
	if suite.Title != "" && suite.Title != suite.File {
		titles = append(append([]string(nil), parents...), suite.Title)
	}
	for _, spec := range suite.Specs {
		title := strings.Join(append(append([]string(nil), titles...), spec.Title), " > ")
		file := spec.File
		if file == "" {
			file = suite.File
		}
		for _, test := range spec.Tests {
			if err := collectPlaywrightTest(res, title, file, test); err != nil {
				return fmt.Errorf("%w (test %q)", err, title)
			}
		}
	}
	for _, child := range suite.Suites {
		if err := collectPlaywrightSuite(res, child, titles); err != nil {
			return err
		}
	}
	return nil
}

func collectPlaywrightTest(res *BrowserResult, title, file string, test playwrightTest) error {
	for _, result := range test.Results {
		for _, att := range result.Attachments {
			if err := collectPlaywrightAttachment(res, att); err != nil {
				return err
			}
		}
	}
	if len(test.Results) == 0 {
		return nil
	}
	// The last result is the final attempt after any retries.
	last := test.Results[len(test.Results)-1]
	switch last.Status {
	case "passed", "skipped", "interrupted", "":
		return nil
	}
	failure := BrowserTestFailure{Title: title, File: file, Status: last.Status}
	if len(last.Errors) > 0 {
		failure.Error = last.Errors[0].Message
	}
	res.FailedTests = append(res.FailedTests, failure)
	return nil
}

func collectPlaywrightAttachment(res *BrowserResult, att playwrightAttachment) error {
	switch {
	case att.Name == playwrightConsoleAttachment:
		if len(att.Body) == 0 {
			return nil
		}
		var items []playwrightConsoleItem
		if err := json.Unmarshal(att.Body, &items); err != nil {
			return fmt.Errorf("fanout: parse playwright %q attachment: %w", att.Name, err)
		}
		for _, it := range items {
			level := it.Type
			if level == "" {
				level = it.Level
			}
			res.Console = append(res.Console, ConsoleEntry{Level: level, Text: it.Text, Location: it.Location})
		}
	case att.Name == playwrightNetworkAttachment:
		if len(att.Body) == 0 {
			return nil
		}
		var items []playwrightNetworkItem
		if err := json.Unmarshal(att.Body, &items); err != nil {
			return fmt.Errorf("fanout: parse playwright %q attachment: %w", att.Name, err)
		}
		for _, it := range items {
			msg := it.Error
			if msg == "" {
				msg = it.Failure
			}
			res.NetworkFailures = append(res.NetworkFailures, NetworkFailure{URL: it.URL, Method: it.Method, Status: it.Status, Error: msg})
		}
	case att.Name == playwrightScreenshotAttachment, strings.HasPrefix(att.ContentType, "image/") && att.Path != "":
		if att.Path == "" && len(att.Body) == 0 {
			return nil
		}
		res.Screenshots = append(res.Screenshots, BrowserScreenshot{Name: att.Name, Path: att.Path, Data: att.Body})
	}
	return nil
}
