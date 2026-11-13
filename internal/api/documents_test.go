package api

import (
	"testing"
)

func TestDocumentFileName(t *testing.T) {
	cases := map[string]string{
		documentFileName("Maya Torres", "Resume", "Microsoft", "pdf"):       "Maya_Torres_Resume_Microsoft.pdf",
		documentFileName("José Núñez", "Cover_Letter", "AT&T Inc.", "docx"): "Jose_Nunez_Cover_Letter_AT_T_Inc.docx",
		documentFileName("", "Resume", "", "pdf"):                           "Resume.pdf",
		documentFileName(`a"b/../c`, "Resume", "\r\nX", "pdf"):              "a_b_c_Resume_X.pdf",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestContactFromResume(t *testing.T) {
	got := contactFromResume("Maya Torres\nTampa, FL | maya.t@email.com | (813) 555-0142\n")
	if len(got) != 2 || got[0] != "maya.t@email.com" || got[1] != "(813) 555-0142" {
		t.Fatalf("got %v", got)
	}
}

func TestClipUTF8(t *testing.T) {
	if got := clip("résumé résumé", 2); got != "r" {
		t.Fatalf("got %q", got)
	}
	if got := clip("ab", 5); got != "ab" {
		t.Fatal(got)
	}
}
