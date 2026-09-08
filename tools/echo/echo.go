// Package echo is the tool-isolation smoke test for the kernel: a plugin
// that declares one channel, journals whatever it is handed, and needs
// nothing from the kernel that the contract does not already name.
package echo

import kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"

const (
	// Namespace is this plugin's identity: it owns namespace.* and its own
	// storage, per the manifest contract.
	Namespace = "echo"
	// PingChannel is the one channel echo declares and subscribes to.
	PingChannel = Namespace + ".ping"
	// SeenJournal is where every delivered payload is recorded.
	SeenJournal = "seen"
	apiVersion  = 1
)

// Manifest is echo's static self-declaration: one PUBSUB channel and an
// interest in it, so publishing to PingChannel is a self-contained round
// trip through the kernel.
func Manifest() *kernelv1.PluginManifest {
	return &kernelv1.PluginManifest{
		Namespace:  Namespace,
		Version:    "0.1.0",
		ApiVersion: apiVersion,
		Channels: []*kernelv1.ChannelDecl{
			{Name: PingChannel, Type: kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB},
		},
		Interests: []*kernelv1.InterestDecl{
			{Kind: &kernelv1.InterestDecl_Channel{
				Channel: &kernelv1.ChannelInterest{Pattern: PingChannel},
			}},
		},
		Description: "Journals every payload delivered on echo.ping.",
	}
}

// SeenAppend turns a delivered payload into the JournalAppend call that
// records it seen. Durability is echo's own decision (sync=true): the slice
// gate counts records exactly, so a crash between append and fsync must not
// be able to produce a silent loss.
func SeenAppend(payload []byte) *kernelv1.JournalAppendRequest {
	return &kernelv1.JournalAppendRequest{
		Journal: SeenJournal,
		Records: [][]byte{payload},
		Sync:    true,
	}
}
