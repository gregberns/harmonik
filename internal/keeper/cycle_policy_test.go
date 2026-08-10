package keeper

import (
	"reflect"
	"testing"
)

func TestDefaultCyclePolicyMatchesLegacyLibraryDefaults(t *testing.T) {
	legacy := CyclerConfig{}
	legacy.applyDefaults()

	got := LegacyCyclerConfig(DefaultCyclePolicy(), CycleEnv{})
	want := cyclePolicyFromConfig(legacy)
	if diff := policyFieldDiff(want, cyclePolicyFromConfig(got)); diff != "" {
		t.Fatal(diff)
	}
}

func TestDefaultCyclePolicyPreservesDisabledSentinels(t *testing.T) {
	got := DefaultCyclePolicy()
	if got.BootGracePeriod != 0 || got.MaxBootGraceTotal != 0 {
		t.Errorf("boot grace = %v/%v; want disabled zero sentinels", got.BootGracePeriod, got.MaxBootGraceTotal)
	}
	if got.OperatorTurnLookback != 0 || got.PostAnswerGrace != 0 {
		t.Errorf("transcript gates = %v/%v; want disabled zero sentinels", got.OperatorTurnLookback, got.PostAnswerGrace)
	}
}

func TestCyclePolicyContainsOnlyPolicyValues(t *testing.T) {
	typ := reflect.TypeOf(CyclePolicy{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type.Kind() == reflect.Func || field.Type.Kind() == reflect.Interface {
			t.Errorf("CyclePolicy.%s has forbidden %s type", field.Name, field.Type.Kind())
		}
	}
}

func policyFieldDiff(want, got CyclePolicy) string {
	if reflect.DeepEqual(want, got) {
		return ""
	}
	return "DefaultCyclePolicy does not match CyclerConfig.applyDefaults field by field"
}
