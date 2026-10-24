package paisu

import (
	"testing"

	"github.com/opensawce/kaasu/i18n"
)

func TestHappyOnlyAfterConfirmation(t *testing.T) {
	if Choose(Inputs{JustConfirmed: true, LiveSources: 1}) != Happy {
		t.Error("confirmation should be happy")
	}
	// Without a fresh confirmation, nothing may be happy.
	for _, in := range []Inputs{
		{LiveSources: 1},
		{LiveSources: 1, ReviewCount: 2},
		{LiveSources: 0},
		{LiveSources: 1, DeadWebhooks: 1, JustConfirmed: true},
	} {
		if Choose(in) == Happy {
			t.Errorf("not happy: %+v", in)
		}
	}
}

func TestAsleepWhenAllSourcesOffline(t *testing.T) {
	if Choose(Inputs{LiveSources: 0}) != Asleep {
		t.Error("no live source should be asleep")
	}
}

func TestWorriedWinsOverEverything(t *testing.T) {
	if Choose(Inputs{LedgerBroken: true, LiveSources: 0, ReviewCount: 3}) != Worried {
		t.Error("broken ledger must be worried")
	}
	if Choose(Inputs{DeadWebhooks: 1, LiveSources: 1}) != Worried {
		t.Error("dead webhook must be worried")
	}
}

func TestNeedsOKAndWaiting(t *testing.T) {
	if Choose(Inputs{LiveSources: 1, ReviewCount: 1}) != NeedsOK {
		t.Error("review item should show needs-ok")
	}
	if Choose(Inputs{LiveSources: 1}) != Waiting {
		t.Error("calm day should be waiting")
	}
}

func TestEveryMoodHasLabelKey(t *testing.T) {
	for _, m := range []Mood{Happy, Waiting, NeedsOK, Asleep, Worried} {
		if Label(m) == "" {
			t.Errorf("%s has no label", m)
		}
	}
}

func TestEveryMoodLabelIsTranslated(t *testing.T) {
	for _, l := range i18n.Languages {
		for _, m := range []Mood{Happy, Waiting, NeedsOK, Asleep, Worried} {
			if got := i18n.T(l, Label(m)); got == Label(m) {
				t.Errorf("%s/%s: label shows the raw key", l, m)
			}
		}
	}
}
