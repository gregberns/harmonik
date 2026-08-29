package daemon

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// ExportedHandlerPausePolicyConfig is a type alias for HandlerPausePolicyConfig
// so tests in package daemon_test can reference the type directly.
//
// Bead ref: hk-37zy8.
type ExportedHandlerPausePolicyConfig = HandlerPausePolicyConfig

// ExportedNewHandlerPausePolicyGoroutine exposes NewHandlerPausePolicyGoroutine
// for tests in package daemon_test.
//
// Bead ref: hk-37zy8.
var ExportedNewHandlerPausePolicyGoroutine = NewHandlerPausePolicyGoroutine

// ExportedPolicyHandleRateLimitStatus invokes the unexported
// handleRateLimitStatus method on a HandlerPausePolicyGoroutine for tests in
// package daemon_test.
//
// Bead ref: hk-37zy8.
func ExportedPolicyHandleRateLimitStatus(p *HandlerPausePolicyGoroutine, ctx context.Context, evt core.Event) error {
	return p.handleRateLimitStatus(ctx, evt)
}

// ExportedPolicyHandleBudgetExhausted invokes the unexported
// handleBudgetExhausted method on a HandlerPausePolicyGoroutine for tests in
// package daemon_test.
//
// Bead ref: hk-37zy8.
func ExportedPolicyHandleBudgetExhausted(p *HandlerPausePolicyGoroutine, ctx context.Context, evt core.Event) error {
	return p.handleBudgetExhausted(ctx, evt)
}

// ExportedNewOperatorPauseController exposes NewOperatorPauseController for
// tests in package daemon_test.
//
// Bead ref: hk-ry8q1.
var ExportedNewOperatorPauseController = NewOperatorPauseController

// ExportedAutoResumeConfig is a type alias for AutoResumeConfig so tests in
// package daemon_test can reference the type directly.
//
// Bead ref: hk-0otqs.
type ExportedAutoResumeConfig = AutoResumeConfig

// ExportedHandlerPauseControllerSchedule exposes HandlerPauseController.Schedule
// for tests in package daemon_test.
//
// Bead ref: hk-0otqs.
func ExportedHandlerPauseControllerSchedule(c *HandlerPauseController, ctx context.Context, agentType core.AgentType, after time.Duration) {
	c.Schedule(ctx, agentType, after)
}

// ExportedHandlerPauseControllerSetAutoResumeCfg exposes
// HandlerPauseController.SetAutoResumeConfig for tests in package daemon_test.
//
// Bead ref: hk-0otqs.
func ExportedHandlerPauseControllerSetAutoResumeCfg(c *HandlerPauseController, agentType core.AgentType, cfg AutoResumeConfig) {
	c.SetAutoResumeConfig(agentType, cfg)
}
