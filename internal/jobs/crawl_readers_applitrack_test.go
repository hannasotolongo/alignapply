package jobs

import (
	"strings"
	"testing"
)

const appliTrackSample = `var VacanciesAreOnThisPage = true
document.write('<div id=\'AppliTrackListContent\'><p align=center class=noprint id=\'p5743_h\'><span class=\'ListHeader\'>Openings as of 11/11/2026<br/></span></p><ul class=\'postingsList\' id=\'p5743_\'><table class=\'title\' style=\'padding: 0px; width:99.2%\'><tr><td id=\'wrapword\' style=\'width: 950px; padding: 5px;\'>Administrative Assistant - 0.5 FTE &amp; Office</td><td style=\'width: 275px;\'><span class=\'title2\'> JobID: 5743 </span></td></tr></table>');
document.write('<div ><li><span class="label">Position Type:</span><br>&nbsp;&nbsp;<span class="normal">Administrative Support/</span><span class="normal">Administrative Assistant</span><br><br></li><li><span class="label">Date Posted:</span><br>&nbsp;&nbsp;<span class="normal">8/24/2026</span><br><br></li><li><span class="label">Location:</span><br>&nbsp;&nbsp;<span class="normal">Huntley Elementary School</span><br><br></li><li><span class="label">Closing Date:</span><br>&nbsp;&nbsp;<span class="normal">12/1/2026</span><br><br></li>');
document.write('<span class="label">Additional Information: &nbsp;</span><span id="DescriptionText5743_" ><span class="normal"><p>The Appleton Area School District, 122 E College Ave, Appleton, WI 54911 has an opening.</p><p>Duties: answer phones.</p></span></span><a href=\'#\'><span style=\'color:#4c4c4c\'>Email To A Friend</span></a><br/>');
document.write('</div></ul><p id=\'p5896_h\'></p><ul class=\'postingsList\' id=\'p5896_\'><table class=\'title\'><tr><td id=\'wrapword\'>Bus Driver</td></tr></table><div><li><span class="label">Location:</span><br><span class="normal">Transportation</span></li><span id="DescriptionText5896_"><span class="normal">Drive routes. Questions: hr@aasd.k12.wi.us</span></span></div></ul>');
`

func TestParseAppliTrackPostings(t *testing.T) {
	postings := parseAppliTrackPostings(appliTrackSample)
	if len(postings) != 2 {
		t.Fatalf("got %d postings, want 2", len(postings))
	}
	first := postings[0]
	if first.ID != "5743" || first.Title != "Administrative Assistant - 0.5 FTE & Office" {
		t.Errorf("first posting = %q %q", first.ID, first.Title)
	}
	if first.Building != "Huntley Elementary School" {
		t.Errorf("building = %q", first.Building)
	}
	if first.Posted == nil || first.Posted.Format("2006-01-02") != "2026-08-24" {
		t.Errorf("posted = %v", first.Posted)
	}
	if first.Closes == nil || first.Closes.Format("2006-01-02") != "2026-12-01" {
		t.Errorf("closes = %v", first.Closes)
	}
	if !strings.Contains(first.Description, "answer phones") || strings.Contains(first.Description, "Email To A Friend") {
		t.Errorf("description = %q", first.Description)
	}
	if postings[1].ID != "5896" || postings[1].Title != "Bus Driver" || postings[1].Building != "Transportation" {
		t.Errorf("second posting = %+v", postings[1])
	}
}

func TestAppliTrackPlace(t *testing.T) {
	city, state := appliTrackPlace(appliTrackUnscript(appliTrackSample), "Appleton Area School District")
	if city != "Appleton" || state != "WI" {
		t.Errorf("place = %q, %q; want Appleton, WI", city, state)
	}
	// No street address: the k12 email gives the state, the name the town.
	city, state = appliTrackPlace("Questions: hr@aasd.k12.wi.us", "Appleton Area School District")
	if city != "Appleton" || state != "WI" {
		t.Errorf("place from name = %q, %q; want Appleton, WI", city, state)
	}
}

func TestAppliTrackBoardParts(t *testing.T) {
	slug := AppliTrackSlug("aasd", "Appleton", "wi")
	district, city, state := appliTrackBoardParts(slug)
	if district != "aasd" || city != "Appleton" || state != "WI" {
		t.Errorf("parts = %q %q %q", district, city, state)
	}
	if got := appliTrackPostingURL("aasd", "5743"); got != "https://www.applitrack.com/aasd/onlineapp/default.aspx?AppliTrackJobId=5743&AppliTrackLayoutMode=detail&AppliTrackViewPosting=1" {
		t.Errorf("posting url = %s", got)
	}
}

func TestDecodeLegacyText(t *testing.T) {
	got := decodeLegacyText([]byte("Caf\xe9 \x96 teacher\x92s aide"))
	if got != "Café – teacher’s aide" {
		t.Errorf("decoded = %q", got)
	}
	if decodeLegacyText([]byte("Café")) != "Café" {
		t.Error("valid UTF-8 should be left alone")
	}
}
