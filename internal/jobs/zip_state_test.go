package jobs

import "testing"

func TestZipState(t *testing.T) {
	for zip, want := range map[string]string{"10001": "NY", "07030": "NJ", "32601": "FL", "78701": "TX", "94103": "CA", "02108": "MA", "20500": "DC", "99501": "AK", "00000": ""} {
		if got := zipState(zip); got != want {
			t.Errorf("zipState(%s) = %q, want %q", zip, got, want)
		}
	}
	if got := StateOfPlace("10001"); got != "NY" {
		t.Errorf("StateOfPlace(10001) = %q, want NY", got)
	}
}
