package handlercontract

// diagnostic_hc014a.go — DiagnosticReport type for Adapter.Diagnose (HC-014a).
//
// DiagnosticReport is the return type of the optional Adapter.Diagnose seam
// declared in specs/handler-contract.md §4.3a HC-014a.
//
// The full schema is deferred.  Today only Message and Healthy
// are populated; no daemon consumer reads them (they are logged for operator
// visibility only).
//
// Spec: specs/handler-contract.md §4.3a HC-014a.
// Bead: hk-tvsl7.

// DiagnosticReport carries the result of an Adapter.Diagnose call.
//
// The full shape is deferred; the controller logs Message at INFO
// and does not act on Healthy.  A later change MAY refuse to resume
// when Healthy is false.
//
// Adapters that do not support diagnostics MUST return ErrDeterministic from
// Diagnose instead of returning a DiagnosticReport.
type DiagnosticReport struct {
	// Message is a human-readable summary of the diagnostic outcome.
	// May be empty when the adapter has nothing meaningful to report.
	Message string

	// Healthy reports whether the handler condition that triggered the pause
	// appears to have resolved.  False means the condition persists or is unknown.
	//
	// This field is informational only; the controller does not gate
	// Resume on its value.  A later change MAY enforce Healthy=true
	// as a precondition for Resume.
	Healthy bool
}
