package keeper

import (
	"context"
	"time"
)

type configPaneWriter struct{ cfg *CyclerConfig }

func (p configPaneWriter) Inject(ctx context.Context, target, value string) error {
	return injectTextClocked(ctx, p.cfg.Clock, target, value)
}
func (p configPaneWriter) SendEscape(context.Context, string) error { return nil }
func (p configPaneWriter) SetEnv(ctx context.Context, target, key, value string) error {
	return SetTmuxEnv(ctx, target, key, value)
}

type configContextStore struct{ cfg *CyclerConfig }

func (s configContextStore) ReadGauge() (*CtxFile, time.Time, error) {
	return ReadCtxFile(s.cfg.ProjectDir, s.cfg.AgentName)
}

func (s configContextStore) SetManagedSession(sid string) error {
	return WriteManagedSessionID(s.cfg.ProjectDir, s.cfg.AgentName, sid)
}

func (s configContextStore) ClearPrecompactTrigger() error {
	return ClearPrecompactTrigger(s.cfg.ProjectDir, s.cfg.AgentName)
}

type configActivityProbe struct{ cfg *CyclerConfig }

func (p configActivityProbe) IdleMarkerModTime() (time.Time, bool) {
	return defaultIdleMarkerModTime(p.cfg.ProjectDir, p.cfg.AgentName)
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
	return recentTranscriptTurn(p.cfg.resolvedTranscriptDir(), sid, role)
}

type configHandoffDocument struct{ cfg *CyclerConfig }

func (d configHandoffDocument) Path() string {
	return defaultHandoffFilePath(d.cfg.ProjectDir, d.cfg.AgentName)
}
func (d configHandoffDocument) Read() (string, error) { return defaultReadHandoff(d.Path()) }
func (d configHandoffDocument) ModTime() (time.Time, bool) {
	return defaultHandoffModTime(d.Path())
}
func (d configHandoffDocument) ScrubNonce() error { return defaultScrubHandoffNonces(d.Path()) }

type configJournalStore struct{ cfg *CyclerConfig }

func (s configJournalStore) path() string { return journalFilePath(s.cfg.ProjectDir, s.cfg.AgentName) }
func (s configJournalStore) Write(j *CycleJournal) error {
	return writeJournalFile(s.path(), j)
}
func (s configJournalStore) Read() (*CycleJournal, error) { return defaultReadJournal(s.path()) }
