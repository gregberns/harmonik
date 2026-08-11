package keeper

import (
	"context"
	"time"
)

type configPaneWriter struct{ cfg *CyclerConfig }

func (p configPaneWriter) Inject(ctx context.Context, target, value string) error {
	return p.cfg.InjectFn(ctx, target, value)
}
func (p configPaneWriter) SendEscape(context.Context, string) error { return nil }
func (p configPaneWriter) SetEnv(ctx context.Context, target, key, value string) error {
	return p.cfg.SetTmuxEnvFn(ctx, target, key, value)
}

type configContextStore struct{ cfg *CyclerConfig }

func (s configContextStore) ReadGauge() (*CtxFile, time.Time, error) {
	return s.cfg.ReadGaugeFn(s.cfg.ProjectDir, s.cfg.AgentName)
}
func (s configContextStore) SetManagedSession(sid string) error {
	return s.cfg.SetManagedSessionFn(s.cfg.ProjectDir, s.cfg.AgentName, sid)
}
func (s configContextStore) ClearPrecompactTrigger() error {
	return s.cfg.ClearPrecompactTriggerFn(s.cfg.ProjectDir, s.cfg.AgentName)
}

type configActivityProbe struct{ cfg *CyclerConfig }

func (p configActivityProbe) IdleMarkerModTime() (time.Time, bool) {
	return p.cfg.IdleMarkerModTimeFn(p.cfg.ProjectDir, p.cfg.AgentName)
}
func (p configActivityProbe) LastUserTurn(sid string) (time.Time, bool) {
	return p.lastTurn(sid, "user")
}
func (p configActivityProbe) LastAssistantTurn(sid string) (time.Time, bool) {
	return p.lastTurn(sid, "assistant")
}
func (p configActivityProbe) lastTurn(sid, role string) (time.Time, bool) {
	if sid == "" {
		return time.Time{}, false
	}
	return p.cfg.recentTurnFn()(p.cfg.resolvedTranscriptDir(), sid, role)
}

type configHandoffDocument struct{ cfg *CyclerConfig }

func (d configHandoffDocument) Path() string {
	return d.cfg.HandoffFilePath(d.cfg.ProjectDir, d.cfg.AgentName)
}
func (d configHandoffDocument) Read() (string, error) { return d.cfg.ReadHandoff(d.Path()) }
func (d configHandoffDocument) ModTime() (time.Time, bool) {
	return d.cfg.HandoffModTimeFn(d.Path())
}
func (d configHandoffDocument) ScrubNonce() error { return d.cfg.TruncateHandoffFn(d.Path()) }

type configJournalStore struct{ cfg *CyclerConfig }

func (s configJournalStore) path() string { return journalFilePath(s.cfg.ProjectDir, s.cfg.AgentName) }
func (s configJournalStore) Write(j *CycleJournal) error {
	return s.cfg.WriteJournalFn(s.path(), j)
}
func (s configJournalStore) Read() (*CycleJournal, error) { return s.cfg.ReadJournalFn(s.path()) }
