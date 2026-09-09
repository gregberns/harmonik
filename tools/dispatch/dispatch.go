// Package dispatch is the first non-toy kernel plugin: a competing-consumers
// work dispatcher. One namespace, two roles chosen at launch. The primary
// accepts submitted payloads, stamps each with its own job identity, and
// hands them to a point-to-point work channel; every worker competes on that
// channel and does the work exactly once. All durable state lives in kernel
// journals, so the plugin process holds nothing a kill -9 can lose.
package dispatch

import (
	"encoding/json"
	"fmt"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

const (
	// Namespace is this plugin's identity: it owns namespace.* and its own
	// storage, per the manifest contract.
	Namespace = "dispatch"
	// WorkChannel is the point-to-point channel the primary forwards jobs on
	// and every worker competes to consume.
	WorkChannel = Namespace + ".work"
	// StatusChannel is the pub/sub channel a worker publishes a completion on.
	StatusChannel = Namespace + ".status"
	// SubmitChannel is the public pub/sub channel callers publish new work on;
	// the primary holds the only interest in it.
	SubmitChannel = Namespace + ".submit"

	// WorkerGroup is the competing-consumer group every worker joins on the
	// work channel, so the kernel load-balances jobs across the members.
	WorkerGroup = "workers"

	// AcceptedJournal records every job the primary accepted (job id + body).
	AcceptedJournal = "accepted"
	// ForwardedJournal records a marker per job id the primary forwarded to
	// the work channel. accepted minus forwarded is the re-forward set on a
	// restart.
	ForwardedJournal = "forwarded"
	// DoneJournal records every job a worker completed; it doubles as the
	// worker's dedupe set, keyed by job id.
	DoneJournal = "done"

	apiVersion = 1
)

// Role is which half of the dispatcher a launched process runs. The kernel
// keeps one dispatch plugin per node; the node that declares the work channel
// runs the primary, and every other node runs a worker.
type Role string

const (
	// RolePrimary accepts submissions, stamps job ids, and forwards work.
	RolePrimary Role = "primary"
	// RoleWorker competes for work and completes it exactly once.
	RoleWorker Role = "worker"
)

// ParseRole turns a launch argument into a Role, refusing anything else so a
// misconfigured launch fails loudly instead of running a silent no-op role.
func ParseRole(s string) (Role, error) {
	switch Role(s) {
	case RolePrimary, RoleWorker:
		return Role(s), nil
	default:
		return "", fmt.Errorf("dispatch: unknown role %q (want %q or %q)", s, RolePrimary, RoleWorker)
	}
}

// Job is the plugin-owned unit of work. ID is dispatch's own identity for the
// job, stamped once on acceptance and persisted. It is NOT the envelope
// message_id: a requeue republishes with a fresh message_id, so only the
// plugin's own id survives a requeue and can anchor the worker's dedupe.
type Job struct {
	ID   string `json:"id"`
	Body []byte `json:"body"`
}

// marshalJob encodes a job for a journal record or a work payload.
func marshalJob(j Job) ([]byte, error) {
	b, err := json.Marshal(j)
	if err != nil {
		return nil, fmt.Errorf("dispatch: marshal job %q: %w", j.ID, err)
	}
	return b, nil
}

// unmarshalJob decodes a job from a journal record or a work payload.
func unmarshalJob(b []byte) (Job, error) {
	var j Job
	if err := json.Unmarshal(b, &j); err != nil {
		return Job{}, fmt.Errorf("dispatch: unmarshal job: %w", err)
	}
	return j, nil
}

// Manifest is the static self-declaration for a role. The primary declares the
// three channels (it is the declaring kernel that owns the work queue) and
// holds the only interest in submit. The worker declares no channel and only
// an interest in the work channel, joined to the worker group.
func Manifest(role Role) *kernelv1.PluginManifest {
	m := &kernelv1.PluginManifest{
		Namespace:  Namespace,
		Version:    "0.1.0",
		ApiVersion: apiVersion,
	}
	switch role {
	case RolePrimary:
		m.Channels = []*kernelv1.ChannelDecl{
			{Name: WorkChannel, Type: kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT},
			{Name: StatusChannel, Type: kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB},
			{Name: SubmitChannel, Type: kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB, Public: true},
		}
		m.Interests = []*kernelv1.InterestDecl{
			{Kind: &kernelv1.InterestDecl_Channel{
				Channel: &kernelv1.ChannelInterest{Pattern: SubmitChannel},
			}},
		}
		m.Description = "Accepts dispatch.submit payloads, stamps a job id, and forwards them on dispatch.work."
	case RoleWorker:
		m.Interests = []*kernelv1.InterestDecl{
			{Kind: &kernelv1.InterestDecl_Channel{
				Channel: &kernelv1.ChannelInterest{Pattern: WorkChannel, Group: WorkerGroup},
			}},
		}
		m.Description = "Competes for dispatch.work, does the work once per job id, reports on dispatch.status."
	}
	return m
}

// acceptedAppend records a newly accepted job. Durability is dispatch's own
// decision (sync=true): the slice gate counts records exactly, so a crash
// between append and fsync must not be able to lose an accepted job.
func acceptedAppend(j Job) (*kernelv1.JournalAppendRequest, error) {
	b, err := marshalJob(j)
	if err != nil {
		return nil, err
	}
	return &kernelv1.JournalAppendRequest{Journal: AcceptedJournal, Records: [][]byte{b}, Sync: true}, nil
}

// forwardedAppend records the marker that a job id reached the work channel.
func forwardedAppend(id string) *kernelv1.JournalAppendRequest {
	return &kernelv1.JournalAppendRequest{Journal: ForwardedJournal, Records: [][]byte{[]byte(id)}, Sync: true}
}

// doneAppend records a completed job; the done journal is also the dedupe set.
func doneAppend(j Job) (*kernelv1.JournalAppendRequest, error) {
	b, err := marshalJob(j)
	if err != nil {
		return nil, err
	}
	return &kernelv1.JournalAppendRequest{Journal: DoneJournal, Records: [][]byte{b}, Sync: true}, nil
}
