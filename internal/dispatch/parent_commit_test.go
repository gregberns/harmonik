package dispatch

import "testing"

func TestValidateParentCommitCanonicalObjectIDs(t *testing.T) {
	for _, value := range []string{
		"0123456789abcdef0123456789abcdef01234567",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	} {
		if err := ValidateParentCommit(value); err != nil {
			t.Fatalf("ValidateParentCommit(%q) = %v", value, err)
		}
	}
	for _, value := range []string{
		"",
		"0123456789abcdef0123456789abcdef0123456",
		"0123456789abcdef0123456789abcdef012345678",
		"0123456789ABCDEF0123456789ABCDEF01234567",
		"g123456789abcdef0123456789abcdef01234567",
	} {
		if err := ValidateParentCommit(value); err == nil {
			t.Fatalf("ValidateParentCommit(%q) = nil", value)
		}
	}
}
