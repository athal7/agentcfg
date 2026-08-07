package render

import "testing"

func TestSubstituteOf_BidirectionalForRegisteredPairs(t *testing.T) {
	pairs := [][2]Capability{
		{CapPrimaryAgent, CapPromptAppend},
		{CapModelLiteralBinding, CapModelClassBinding},
		{CapBashUnorderedMap, CapBashOrderedList},
	}
	for _, pair := range pairs {
		got, ok := SubstituteOf(pair[0])
		if !ok || got != pair[1] {
			t.Errorf("SubstituteOf(%s) = (%s, %v), want (%s, true)", pair[0], got, ok, pair[1])
		}
		got, ok = SubstituteOf(pair[1])
		if !ok || got != pair[0] {
			t.Errorf("SubstituteOf(%s) = (%s, %v), want (%s, true)", pair[1], got, ok, pair[0])
		}
	}
}

func TestSubstituteOf_NoSubstituteReturnsFalse(t *testing.T) {
	if _, ok := SubstituteOf(CapAgentDefinitions); ok {
		t.Error("SubstituteOf(CapAgentDefinitions) = ok, want no registered substitute")
	}
}
