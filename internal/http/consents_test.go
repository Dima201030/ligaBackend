package httpapi

import "testing"

func TestConsentDocumentVersionSet(t *testing.T) {
	if ConsentDocumentVersion == "" {
		t.Fatal("consent document version must be set")
	}
}
