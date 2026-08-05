package readiness

// validate_test.go — the claims the validator makes.
//
// Two of these are structural rather than behavioural, and they are the ones
// that matter most: that Validate has no way to reach the fleet, and that a
// snapshot arriving from a file is re-checked rather than trusted for having
// the right Go type.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var validatedAt = time.Date(2026, 8, 4, 18, 0, 0, 0, time.UTC)

// safePlan is a run shape the gate accepts. Each rejection test breaks exactly
// one thing in it.
func safePlan() RunPlan {
	return RunPlan{
		Harness:         "claude",
		RepoTarget:      "/tmp/scratch-clone",
		ScratchRepo:     "/tmp/scratch-clone",
		QueueKind:       QueueKindStream,
		FeedbackEnabled: false,
		Concurrency:     1,
		ItemCount:       1,
	}
}

func quietHost() HostFacts {
	return HostFacts{LoadAverage: 4.5, CPUCount: 10, FreeDiskGB: 120, DaemonsAlive: 1}
}

func goodSnapshot(t *testing.T) Snapshot {
	t.Helper()
	snap, err := newSnapshot(wholeRequest())
	if err != nil {
		t.Fatalf("newSnapshot: %v", err)
	}
	return snap
}

func TestValidate_AcceptsALocalStreamRunOnAQuietHost(t *testing.T) {
	got := Validate(goodSnapshot(t), safePlan(), quietHost(), DefaultHostLimits, validatedAt)

	if !got.Accepted {
		t.Fatalf("safe plan on a quiet host was rejected: %+v", got.Rejections)
	}
	if len(got.Rejections) != 0 {
		t.Errorf("rejections = %+v, want none", got.Rejections)
	}
	if !reflect.DeepEqual(got.SelectedBeads, []string{"hk-canary"}) {
		t.Errorf("selected beads = %q", got.SelectedBeads)
	}
	// The record must carry the bar it applied, or a result read later cannot
	// say which bar it cleared.
	if got.AppliedLimits != DefaultHostLimits {
		t.Errorf("applied limits = %+v, want them recorded", got.AppliedLimits)
	}
	if !got.ValidatedAt.Equal(validatedAt) {
		t.Errorf("validated at = %s, want the injected time", got.ValidatedAt)
	}
}

// The operator overruled the one-item rule: a queue that can carry only one
// item at a time proves nothing worth proving, and the assessor's job is to sign
// off on several items running at once. This is the test that says so.
//
// It is deliberately a full accept and not a "no concurrency rejection" probe.
// A test that only checked one reason was absent would stay green if some other
// clause quietly took over the refusal.
func TestValidate_AcceptsSeveralItemsRunningAtTheSameTime(t *testing.T) {
	snap := multiItemSnapshot(t)
	plan := safePlan()
	plan.ItemCount = 3
	plan.Concurrency = 3

	got := Validate(snap, plan, quietHost(), DefaultHostLimits, validatedAt)
	if !got.Accepted {
		t.Fatalf("three items at concurrency three were rejected: %+v", got.Rejections)
	}
	// The evidence must RECORD the shape, not merely tolerate it.
	if got.Plan.ItemCount != 3 || got.Plan.Concurrency != 3 {
		t.Errorf("record states items=%d concurrency=%d, want 3 and 3", got.Plan.ItemCount, got.Plan.Concurrency)
	}
	if len(got.SelectedBeads) != 3 {
		t.Errorf("selected beads = %q, want all three named", got.SelectedBeads)
	}
}

// The rejection matrix T9 names, one case per clause. Concurrency and item
// count are no longer on it as "not one"; they are on it as "not stated".
func TestValidate_RejectsEveryRunShapePassOneExcludes(t *testing.T) {
	for name, tc := range map[string]struct {
		damage func(*RunPlan)
		want   RejectionReason
	}{
		"pi harness":          {func(p *RunPlan) { p.Harness = "pi" }, RejectPiHarness},
		"remote worker":       {func(p *RunPlan) { p.RemoteWorker = "dgx" }, RejectRemoteWorker},
		"cross repository":    {func(p *RunPlan) { p.RepoTarget = "/Users/gb/github/harmonik" }, RejectCrossRepository},
		"wave queue":          {func(p *RunPlan) { p.QueueKind = QueueKindWave }, RejectWaveQueue},
		"queue kind unset":    {func(p *RunPlan) { p.QueueKind = "" }, RejectQueueKindUnset},
		"feedback on":         {func(p *RunPlan) { p.FeedbackEnabled = true }, RejectFeedbackEnabled},
		"concurrency unset":   {func(p *RunPlan) { p.Concurrency = 0 }, RejectConcurrencyUnset},
		"item count unset":    {func(p *RunPlan) { p.ItemCount = 0 }, RejectItemCountUnset},
		"negative item count": {func(p *RunPlan) { p.ItemCount = -1 }, RejectItemCountUnset},
	} {
		plan := safePlan()
		tc.damage(&plan)
		got := Validate(goodSnapshot(t), plan, quietHost(), DefaultHostLimits, validatedAt)

		if got.Accepted {
			t.Errorf("%s: accepted, want rejected", name)
			continue
		}
		if !hasReason(got.Rejections, tc.want) {
			t.Errorf("%s: rejections = %+v, want one with reason %q", name, got.Rejections, tc.want)
		}
		for _, r := range got.Rejections {
			if r.Detail == "" {
				t.Errorf("%s: rejection %q carries no detail; the reader would have to go measure it again", name, r.Reason)
			}
		}
	}
}

// A gate result from a loaded box is not evidence, green or red, so host
// contention is a refusal and not a warning.
func TestValidate_RejectsAHostThatCannotProduceEvidence(t *testing.T) {
	for name, tc := range map[string]struct {
		damage func(*HostFacts)
		want   RejectionReason
	}{
		"load above the ceiling": {func(h *HostFacts) { h.LoadAverage = 14 }, RejectHostLoad},
		"disk below the floor":   {func(h *HostFacts) { h.FreeDiskGB = 3 }, RejectHostDisk},
		"a second daemon alive":  {func(h *HostFacts) { h.DaemonsAlive = 2 }, RejectDaemonCount},
	} {
		host := quietHost()
		tc.damage(&host)
		got := Validate(goodSnapshot(t), safePlan(), host, DefaultHostLimits, validatedAt)

		if got.Accepted {
			t.Errorf("%s: accepted, want rejected", name)
		}
		if !hasReason(got.Rejections, tc.want) {
			t.Errorf("%s: rejections = %+v, want %q", name, got.Rejections, tc.want)
		}
	}
}

// An unmeasured CPU count would make "load is below CPUs x limit" true for free.
// That is the shape where a guard silently stops guarding.
//
// The case that matters is a host where NOTHING was measured — zero CPUs and
// zero load — because that is what an unmeasured host actually looks like. A
// fixture with zero CPUs but a real load figure passes even with the guard
// deleted, since a positive load beats a zero ceiling by arithmetic rather than
// by the check. The first draft of this test used exactly that fixture and
// proved nothing; a mutation caught it.
func TestValidate_RefusesToJudgeLoadAgainstAnUnmeasuredCPUCount(t *testing.T) {
	for name, host := range map[string]HostFacts{
		"nothing measured":       {CPUCount: 0, LoadAverage: 0, FreeDiskGB: 120, DaemonsAlive: 1},
		"load without CPU count": {CPUCount: 0, LoadAverage: 4.5, FreeDiskGB: 120, DaemonsAlive: 1},
	} {
		got := Validate(goodSnapshot(t), safePlan(), host, DefaultHostLimits, validatedAt)
		if got.Accepted {
			t.Errorf("%s: accepted with no CPU count measured; the load check passed for free", name)
		}
		if !hasReason(got.Rejections, RejectHostLoad) {
			t.Errorf("%s: rejections = %+v, want the load refusal", name, got.Rejections)
		}
	}
}

// The snapshot recorded the posture the items were judged against. If the plan
// disagrees, one of the two is stale and guessing which is worse than refusing.
func TestValidate_RejectsAPlanThatContradictsTheSnapshotPosture(t *testing.T) {
	snap := goodSnapshot(t)
	plan := safePlan()
	plan.RemoteWorker = "dgx" // snapshot judged the item local

	got := Validate(snap, plan, quietHost(), DefaultHostLimits, validatedAt)
	if !hasReason(got.Rejections, RejectPostureMismatch) {
		t.Errorf("rejections = %+v, want the posture mismatch", got.Rejections)
	}
}

// A plan for three items judged against a snapshot that names one is the exact
// mistake the widened record exists to catch. The concurrency rule going away
// must not take this cross-check with it.
func TestValidate_RejectsAManyItemPlanAgainstAOneItemSnapshot(t *testing.T) {
	plan := safePlan()
	plan.ItemCount = 3
	plan.Concurrency = 3

	got := Validate(goodSnapshot(t), plan, quietHost(), DefaultHostLimits, validatedAt)
	if got.Accepted {
		t.Fatal("accepted a three-item plan against a snapshot that judged one item")
	}
	if !hasReason(got.Rejections, RejectPostureMismatch) {
		t.Errorf("rejections = %+v, want the posture mismatch", got.Rejections)
	}
}

// A snapshot with no selected item leaves nothing to judge the plan against. It
// cannot arrive from [Capture], but it can arrive from a zero value, and a
// validator that accepted one would report a pass over an empty record.
func TestValidate_RejectsASnapshotThatNamesNoSelectedItem(t *testing.T) {
	got := Validate(Snapshot{}, safePlan(), quietHost(), DefaultHostLimits, validatedAt)
	if got.Accepted {
		t.Fatal("accepted a plan against an empty snapshot")
	}
	if !hasReason(got.Rejections, RejectSnapshotNoSelected) {
		t.Errorf("rejections = %+v, want the no-selected-item refusal", got.Rejections)
	}
}

// The limits are inputs so the operator's numbers land in the record rather
// than in a source edit. Same host, different bar, different verdict.
func TestValidate_AppliesTheLimitsItWasGivenAndNotABakedInNumber(t *testing.T) {
	host := quietHost() // load 4.5 across 10 CPUs

	loose := Validate(goodSnapshot(t), safePlan(), host, HostLimits{MaxLoadPerCPU: 1.0, MinFreeDiskGB: 10, MaxDaemons: 1}, validatedAt)
	strict := Validate(goodSnapshot(t), safePlan(), host, HostLimits{MaxLoadPerCPU: 0.1, MinFreeDiskGB: 10, MaxDaemons: 1}, validatedAt)

	if !loose.Accepted {
		t.Errorf("loose bar rejected a quiet host: %+v", loose.Rejections)
	}
	if strict.Accepted {
		t.Error("strict bar accepted a host at 0.45 load per CPU against a 0.1 ceiling")
	}
	if strict.AppliedLimits.MaxLoadPerCPU != 0.1 {
		t.Errorf("strict run recorded limits %+v, want the ones it was given", strict.AppliedLimits)
	}
}

// The ratchet on "the validator makes no fleet-daemon or Beads call".
//
// Validate takes plain values. It has no interface parameter to call through
// and no context to carry a deadline for a call it cannot make. That is the
// whole guarantee, and this test is what keeps it: give Validate a ledger, a
// client, or a context and it goes red.
func TestValidateTakesNothingItCouldCallTheFleetWith(t *testing.T) {
	fn := reflect.TypeOf(Validate)
	if fn.NumIn() == 0 {
		t.Fatal("Validate takes no arguments at all, so this test would pass against an empty signature")
	}

	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	for i := range fn.NumIn() {
		param := fn.In(i)
		if param.Kind() == reflect.Interface {
			t.Errorf("Validate parameter %d is the interface %s. "+
				"A validator that can be handed an interface can be handed a live ledger.", i, param)
		}
		if param == ctxType {
			t.Errorf("Validate parameter %d is a context.Context, which only a call that reaches the world needs", i)
		}
		if containsInterfaceField(param) {
			t.Errorf("Validate parameter %d (%s) has a struct field of interface type, "+
				"which is a port smuggled in behind a value", i, param)
		}
	}
}

// containsInterfaceField reports whether t reaches an interface or a func at
// any depth. A port hidden one level down is still a port.
//
// It unwraps slices, arrays, pointers, maps and channels before testing for a
// struct, and the first draft did not. That draft was blind in the direction
// this ratchet is most likely to be defeated: Snapshot reaches Exclusion,
// Command, StaleFinding, CurrentFinding and PendingIntent ONLY through slices,
// so an interface field added to any of those five would have gone unseen. A
// func field is the other smuggling route, since a closure carries whatever it
// captured.
func containsInterfaceField(t reflect.Type) bool {
	return reachesPort(t, make(map[reflect.Type]bool))
}

func reachesPort(t reflect.Type, seen map[reflect.Type]bool) bool {
	// A self-referential type would otherwise recurse forever.
	if seen[t] {
		return false
	}
	seen[t] = true

	switch t.Kind() {
	case reflect.Interface, reflect.Func:
		return true
	case reflect.Slice, reflect.Array, reflect.Ptr, reflect.Map, reflect.Chan:
		if t.Kind() == reflect.Map && reachesPort(t.Key(), seen) {
			return true
		}
		return reachesPort(t.Elem(), seen)
	case reflect.Struct:
		for i := range t.NumField() {
			if reachesPort(t.Field(i).Type, seen) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// encoding/json writes straight into fields and goes through no constructor, so
// a snapshot read back from disk has been checked by nothing at all.
func TestDecodeSnapshot_RechecksARecordThatNeverWentThroughTheConstructor(t *testing.T) {
	// A snapshot that is well-formed JSON and a valid Go Snapshot, but which
	// violates BI-013e: a selected item is closed.
	bad := goodSnapshot(t)
	bad.Selected[0].Candidate.Status = "closed"
	body, err := json.Marshal(bad)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if _, err := DecodeSnapshot(bytes.NewReader(body)); err == nil {
		t.Fatal("DecodeSnapshot accepted a snapshot whose selected item is closed")
	} else if !strings.Contains(err.Error(), "BI-013e") {
		t.Errorf("err = %v, want it to name the requirement that was not satisfied", err)
	}

	// Positive evidence that the decoder works at all, so the refusal above is
	// the check firing and not the decoder being broken.
	good, err := json.Marshal(goodSnapshot(t))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	round, err := DecodeSnapshot(bytes.NewReader(good))
	if err != nil {
		t.Fatalf("DecodeSnapshot on a good record: %v", err)
	}
	if len(round.Selected) != 1 || round.Selected[0].Candidate.BeadID != "hk-canary" {
		t.Errorf("decoded selected items = %+v", round.Selected)
	}
}

// A many-item record has to survive the file, not just the constructor. The
// assessor reads the file.
func TestDecodeSnapshot_KeepsEverySelectedItemAndItsOwnReasonThroughTheFile(t *testing.T) {
	body, err := json.Marshal(multiItemSnapshot(t))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	round, err := DecodeSnapshot(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("DecodeSnapshot: %v", err)
	}
	if len(round.Selected) != 3 {
		t.Fatalf("decoded %d selected items, want 3", len(round.Selected))
	}
	seen := map[string]bool{}
	for _, sel := range round.Selected {
		if sel.RepeatSafeReason == "" {
			t.Errorf("%s lost its repeat-safe reason", sel.Candidate.BeadID)
		}
		if seen[sel.RepeatSafeReason] {
			t.Errorf("%s shares a reason with an earlier item; each is its own judgement", sel.Candidate.BeadID)
		}
		seen[sel.RepeatSafeReason] = true
	}
	if round.Posture.ItemCount != 3 {
		t.Errorf("posture item count = %d, want 3", round.Posture.ItemCount)
	}
}

func TestDecodeSnapshot_RefusesInputThatIsNotASnapshotAtAll(t *testing.T) {
	if _, err := DecodeSnapshot(strings.NewReader("{not json")); err == nil {
		t.Error("DecodeSnapshot accepted malformed JSON")
	}
	if _, err := DecodeSnapshot(strings.NewReader("{}")); err == nil {
		t.Error("DecodeSnapshot accepted an empty object")
	}
}

// A version-1 file names its one item in a `selection` field this build no
// longer reads, so it would decode into a record with no selected item. Say so
// in the refusal rather than report an empty selection.
func TestDecodeSnapshot_RefusesAVersionOneFileByNameRatherThanReadingItEmpty(t *testing.T) {
	const versionOne = `{
	  "schema_version": 1,
	  "captured_at": "2026-08-04T17:30:00Z",
	  "selection": {
	    "candidate": {"bead_id": "hk-canary", "title": "t", "status": "open"},
	    "repeat_safe_reason": "single-file comment edit",
	    "posture": {"local": true, "item_count": 1, "concurrency": 1}
	  },
	  "excluded": [],
	  "commands": [{"argv": ["br", "show", "hk-canary"]}],
	  "events": {"log_paths": [".harmonik/events/events.jsonl"], "note": "` + EventEvidenceNote + `"},
	  "terminal_intent": {"dir": ".harmonik/beads-intents", "pending": []},
	  "stale_findings": [],
	  "current_findings": []
	}`

	_, err := DecodeSnapshot(strings.NewReader(versionOne))
	if !errors.Is(err, ErrSnapshotSchemaUnreadable) {
		t.Fatalf("err = %v, want ErrSnapshotSchemaUnreadable", err)
	}
	if !strings.Contains(err.Error(), "selected") {
		t.Errorf("err = %v, want it to name the field that moved", err)
	}
}

func TestWriteValidation_LeavesAFileTheAssessorCanDecode(t *testing.T) {
	v := Validate(goodSnapshot(t), safePlan(), quietHost(), DefaultHostLimits, validatedAt)
	path := filepath.Join(t.TempDir(), "evidence", "validation.json")

	if err := WriteValidation(path, v); err != nil {
		t.Fatalf("WriteValidation: %v", err)
	}
	body, err := os.ReadFile(path) //nolint:gosec // G304: this test's own temp dir
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var got Validation
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Accepted || !reflect.DeepEqual(got.SelectedBeads, []string{"hk-canary"}) {
		t.Errorf("validation after round trip = %+v", got)
	}
}

func hasReason(rejections []Rejection, want RejectionReason) bool {
	for _, r := range rejections {
		if r.Reason == want {
			return true
		}
	}
	return false
}

// A zero-value HostLimits disables every host check, and two of the three fail
// OPEN: an empty disk clears a zero floor and no daemon clears a zero ceiling.
// That is the one input a caller will actually get wrong.
func TestValidate_RefusesAHostBarThatWasNeverSet(t *testing.T) {
	emptyDisk := HostFacts{CPUCount: 10, LoadAverage: 0, FreeDiskGB: 0, DaemonsAlive: 0}

	got := Validate(goodSnapshot(t), safePlan(), emptyDisk, HostLimits{}, validatedAt)
	if got.Accepted {
		t.Fatal("accepted a host with no free disk against an unset bar; the gate failed open")
	}
	if !hasReason(got.Rejections, RejectNoHostLimits) {
		t.Errorf("rejections = %+v, want the unset-bar refusal", got.Rejections)
	}
}

// An unmeasured CPU count must not stop the checks that do not depend on it, or
// the record understates how bad the host is.
func TestValidate_StillJudgesDiskAndDaemonsWhenTheCPUCountIsMissing(t *testing.T) {
	bad := HostFacts{CPUCount: 0, LoadAverage: 0, FreeDiskGB: 2, DaemonsAlive: 3}

	got := Validate(goodSnapshot(t), safePlan(), bad, DefaultHostLimits, validatedAt)
	for _, want := range []RejectionReason{RejectHostLoad, RejectHostDisk, RejectDaemonCount} {
		if !hasReason(got.Rejections, want) {
			t.Errorf("rejections = %+v, want %q among them", got.Rejections, want)
		}
	}
}

// The Pi harness reaches a remote endpoint, so a Pi plan is not local. Without
// that, this file would hold two definitions of "local" and the posture check
// would read the weaker one.
func TestValidate_CountsAPiPlanAsRemoteForThePostureCheck(t *testing.T) {
	plan := safePlan()
	plan.Harness = "pi" // no named worker, but still not local

	got := Validate(goodSnapshot(t), plan, quietHost(), DefaultHostLimits, validatedAt)
	if !hasReason(got.Rejections, RejectPostureMismatch) {
		t.Errorf("rejections = %+v, want the posture mismatch; the snapshot judged the item local", got.Rejections)
	}
}

// All three posture fields are cross-checked, not two. A field with nothing to
// compare against drifts in silence.
func TestValidate_RejectsAnItemCountThatDisagreesWithTheSnapshotPosture(t *testing.T) {
	plan := safePlan()
	plan.ItemCount = 4

	got := Validate(goodSnapshot(t), plan, quietHost(), DefaultHostLimits, validatedAt)
	if !hasReason(got.Rejections, RejectPostureMismatch) {
		t.Errorf("rejections = %+v, want the posture mismatch on item count", got.Rejections)
	}
}

// A concurrency that disagrees with the snapshot is still a refusal even though
// no particular number is required any more.
func TestValidate_RejectsAConcurrencyThatDisagreesWithTheSnapshotPosture(t *testing.T) {
	plan := safePlan()
	plan.Concurrency = 4

	got := Validate(goodSnapshot(t), plan, quietHost(), DefaultHostLimits, validatedAt)
	if got.Accepted {
		t.Fatal("accepted a plan whose concurrency the snapshot never judged")
	}
	if !hasReason(got.Rejections, RejectPostureMismatch) {
		t.Errorf("rejections = %+v, want the posture mismatch on concurrency", got.Rejections)
	}
}

// validation.json is read with jq. `.rejections | length` over a null errors
// instead of returning zero.
func TestValidate_WritesAnEmptyRejectionListAndNeverNull(t *testing.T) {
	got := Validate(goodSnapshot(t), safePlan(), quietHost(), DefaultHostLimits, validatedAt)
	if !got.Accepted {
		t.Fatalf("fixture no longer accepts: %+v", got.Rejections)
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), `"rejections":null`) {
		t.Errorf("accepted validation encodes rejections as null: %s", body)
	}
}

// Refuse, do not repair. Both cases below were silently corrected by an earlier
// draft: a future schema version was downgraded to this build's, and a tampered
// observational note was rewritten to the constant. For a file whose whole job
// is to be evidence, quietly rewriting it is the defect.
func TestDecodeSnapshot_RefusesARecordItWouldOtherwiseHaveSilentlyRewritten(t *testing.T) {
	for name, tc := range map[string]struct {
		damage func(*Snapshot)
		want   error
	}{
		"schema version from a later build": {func(s *Snapshot) { s.SchemaVersion = SchemaVersion + 1 }, ErrSnapshotSchemaUnreadable},
		"no schema version at all":          {func(s *Snapshot) { s.SchemaVersion = 0 }, ErrSnapshotSchemaUnreadable},
		"observational note tampered with":  {func(s *Snapshot) { s.Events.Note = "TAMPERED: ledger writes OK" }, ErrSnapshotNoteAltered},
	} {
		snap := goodSnapshot(t)
		tc.damage(&snap)
		body, err := json.Marshal(snap)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		if _, err := DecodeSnapshot(bytes.NewReader(body)); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

// The ratchet's own reach, tested directly. Snapshot holds every one of its
// nested types behind a slice, so a checker that stopped at the first non-struct
// would be blind exactly where it matters.
func TestTheFleetAccessRatchetSeesPortsBehindSlicesPointersAndFuncs(t *testing.T) {
	type leaf struct{ Port BeadReader }

	for name, tc := range map[string]struct {
		typ  reflect.Type
		want bool
	}{
		"direct interface field": {reflect.TypeOf(struct{ P BeadReader }{}), true},
		"interface via slice":    {reflect.TypeOf(struct{ L []leaf }{}), true},
		"interface via pointer":  {reflect.TypeOf(struct{ L *leaf }{}), true},
		"interface via map":      {reflect.TypeOf(struct{ M map[string]leaf }{}), true},
		"interface via array":    {reflect.TypeOf(struct{ A [2]leaf }{}), true},
		"func field":             {reflect.TypeOf(struct{ F func() error }{}), true},
		"plain values only":      {reflect.TypeOf(RunPlan{}), false},
		"the host facts":         {reflect.TypeOf(HostFacts{}), false},
		"a whole snapshot":       {reflect.TypeOf(Snapshot{}), false},
	} {
		if got := containsInterfaceField(tc.typ); got != tc.want {
			t.Errorf("%s: containsInterfaceField(%s) = %t, want %t", name, tc.typ, got, tc.want)
		}
	}
}
