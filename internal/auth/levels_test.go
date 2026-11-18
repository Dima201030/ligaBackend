package auth

import "testing"

func TestValidLevel(t *testing.T) {
	t.Parallel()
	if !ValidLevel("rookies") {
		t.Fatal("rookies should be valid")
	}
	if ValidLevel("juniors") {
		t.Fatal("juniors should be invalid")
	}
}
