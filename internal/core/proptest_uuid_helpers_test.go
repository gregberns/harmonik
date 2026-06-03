package core

import (
	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// drawNonNilUUID draws a random uuid.UUID guaranteed not to equal uuid.Nil.
// Packs a rapid-drawn int64 >= 1 into the low 8 bytes; high 8 bytes are zero
// but that is fine — uuid.Nil requires ALL bytes to be zero.
func drawNonNilUUID(rt *rapid.T, label string) uuid.UUID {
	n := rapid.Int64Range(1, 1<<62).Draw(rt, label)
	var b [16]byte
	b[8] = byte(n >> 56)
	b[9] = byte(n >> 48)
	b[10] = byte(n >> 40)
	b[11] = byte(n >> 32)
	b[12] = byte(n >> 24)
	b[13] = byte(n >> 16)
	b[14] = byte(n >> 8)
	b[15] = byte(n)
	return uuid.UUID(b)
}

// drawValidAgentType draws one of the declared AgentType constants.
func drawValidAgentType(rt *rapid.T, label string) AgentType {
	return rapid.SampledFrom([]AgentType{
		AgentTypeClaudeCode,
		AgentTypePi,
		AgentTypeClaudeTwin,
		AgentTypePiTwin,
	}).Draw(rt, label)
}

// drawValidFailureClass draws one of the declared FailureClass constants.
func drawValidFailureClass(rt *rapid.T, label string) FailureClass {
	return rapid.SampledFrom([]FailureClass{
		FailureClassTransient,
		FailureClassStructural,
		FailureClassDeterministic,
		FailureClassCanceled,
		FailureClassBudgetExhausted,
		FailureClassCompilationLoop,
	}).Draw(rt, label)
}

// drawValidHandlerPauseCause draws a fully-valid HandlerPauseCause.
func drawValidHandlerPauseCause(rt *rapid.T, label string) HandlerPauseCause {
	return HandlerPauseCause{
		FailureClass: drawValidFailureClass(rt, label+"_fc"),
		SubReason:    rapid.StringN(1, 64, -1).Draw(rt, label+"_sub"),
		SourceRunID:  rapid.StringN(1, 64, -1).Draw(rt, label+"_src_run"),
		SourceBeadID: rapid.StringN(1, 64, -1).Draw(rt, label+"_src_bead"),
		TrippedAt:    rapid.StringN(1, 64, -1).Draw(rt, label+"_at"),
	}
}
