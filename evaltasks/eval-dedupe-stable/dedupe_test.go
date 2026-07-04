package evaldedupestable_test

import (
	"reflect"
	"testing"

	evaldedupestable "github.com/gregberns/harmonik/evaltasks/eval-dedupe-stable"
)

func TestDedupe(t *testing.T) {
	t.Parallel()

	t.Run("order_preserved", func(t *testing.T) {
		t.Parallel()
		got := evaldedupestable.Dedupe([]int{3, 1, 3, 2, 1})
		want := []int{3, 1, 2}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Dedupe([3,1,3,2,1]) = %v, want %v", got, want)
		}
	})

	t.Run("nil_input", func(t *testing.T) {
		t.Parallel()
		got := evaldedupestable.Dedupe(nil)
		if len(got) != 0 {
			t.Errorf("Dedupe(nil) = %v, want empty", got)
		}
	})

	t.Run("empty_input", func(t *testing.T) {
		t.Parallel()
		got := evaldedupestable.Dedupe([]int{})
		if len(got) != 0 {
			t.Errorf("Dedupe([]) = %v, want empty", got)
		}
	})

	t.Run("no_duplicates", func(t *testing.T) {
		t.Parallel()
		got := evaldedupestable.Dedupe([]int{1, 2, 3})
		want := []int{1, 2, 3}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Dedupe([1,2,3]) = %v, want %v", got, want)
		}
	})

	t.Run("all_same", func(t *testing.T) {
		t.Parallel()
		got := evaldedupestable.Dedupe([]int{5, 5, 5})
		want := []int{5}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Dedupe([5,5,5]) = %v, want %v", got, want)
		}
	})
}
