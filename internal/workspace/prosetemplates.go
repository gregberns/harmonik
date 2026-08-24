package workspace

import (
	"embed"
	"strings"
	"text/template"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// The three markdown artifacts the daemon drops into a worktree — agent-task.md,
// reviewer-feedback.iter-N.md and review-target.md — are the text every agent
// reads. That prose is the product, so it lives in templates/ as markdown a
// human can edit without touching Go.
//
//go:embed templates/*.md.tmpl
var proseTemplateFS embed.FS

// proseTemplates is parsed ONCE, at package init. A malformed template is a
// build-time-shaped failure that stops the process here rather than producing a
// wrong agent brief at dispatch time.
var proseTemplates = template.Must(
	template.New("prose").Funcs(proseTemplateFuncs()).ParseFS(proseTemplateFS, "templates/*.md.tmpl"),
)

func proseTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		// ensureNL reproduces the "write the field, then add a newline only if
		// the caller's text does not already end in one" rule that every
		// free-text field (bead body, extra context, notes, hints) follows.
		"ensureNL": ensureTrailingNewline,
		// trimmed backs `{{if trimmed .Field}}` — a section that renders only
		// when its optional text is more than whitespace.
		"trimmed": strings.TrimSpace,
		// jobSummary and isProcessExit read the launching harness's completion
		// mode, which is an enum in another package and so cannot carry a
		// method this package could call from a template.
		"jobSummary":    jobSummary,
		"isProcessExit": completionIsProcessExit,
		// resumeFeedbackPath derives the implementer-resume feedback path when
		// the caller did not supply one.
		"resumeFeedbackPath": resumeFeedbackPath,
		// fromReviewer decides between the reviewer and the daemon heading of
		// the feedback file (hk-2f3v4).
		"fromReviewer": VerdictCameFromAReviewer,
	}
}

// renderProse executes one embedded prose template.
//
// Execution can only fail on a template bug: every template is parsed at package
// init and the data is a plain struct with no methods. An empty return is
// therefore the loud outcome — every writer in this package refuses to write an
// empty artifact and returns ErrTaskFileEmpty instead.
func renderProse(name string, data any) string {
	var sb strings.Builder
	if err := proseTemplates.ExecuteTemplate(&sb, name, data); err != nil {
		return ""
	}
	return sb.String()
}

func ensureTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func completionIsProcessExit(mode handlercontract.CompletionMode) bool {
	return mode == handlercontract.CompletionProcessExit
}

func jobSummary(mode handlercontract.CompletionMode) string {
	if mode == handlercontract.CompletionProcessExit {
		return "implement and commit"
	}
	return "implement, commit, and `/quit`"
}

// resumeFeedbackPath returns the reviewer-feedback path an implementer-resume
// brief points at: the caller-supplied path when there is one, else the file the
// daemon actually wrote for the prior iteration.
//
// The derived branch used to name .harmonik/review.iter-<N-1>.json. Only
// ArchiveVerdict ever wrote that file and no caller ever ran ArchiveVerdict, so
// every resume brief the system has produced pointed at a path that does not
// exist. The real artifact is reviewer-feedback.iter-<N-1>.md, which
// WriteReviewerFeedback writes and which the pi/codex seed prompt already names
// (internal/harness/shared/seedprompt.go).
func resumeFeedbackPath(p AgentTaskPayload) string {
	if p.PriorVerdictFile != "" {
		return p.PriorVerdictFile
	}
	priorN := p.Iteration - 1
	if priorN < 1 {
		priorN = 1
	}
	return ReviewerFeedbackPath(p.WorkspacePath, priorN)
}
