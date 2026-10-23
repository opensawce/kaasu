// Package paisu chooses the mascot's mood for merchant screens. Customer pages never show Paisu.
package paisu

// Mood is one of the five faces.
type Mood string

const (
	Happy   Mood = "happy"    // only right after a confirmation, for a few seconds
	Waiting Mood = "waiting"  // nothing needs attention
	NeedsOK Mood = "needs-ok" // a held payment waits for the owner
	Asleep  Mood = "asleep"   // every source is offline
	Worried Mood = "worried"  // a webhook is dead or the ledger does not verify
)

// Inputs are the facts the mood is chosen from.
type Inputs struct {
	JustConfirmed bool // an order was confirmed in the last few seconds
	ReviewCount   int
	LiveSources   int
	DeadWebhooks  int
	LedgerBroken  bool
}

// Choose returns the mood. The order of checks is the priority: danger first, then sleep, then the owner's work.
func Choose(in Inputs) Mood {
	switch {
	case in.LedgerBroken || in.DeadWebhooks > 0:
		return Worried
	case in.LiveSources == 0:
		return Asleep
	case in.ReviewCount > 0:
		return NeedsOK
	case in.JustConfirmed:
		return Happy
	}
	return Waiting
}

// Label is the text read for screen readers, from the catalogue key paisu.<mood>.
func Label(m Mood) string { return "paisu." + string(m) }
