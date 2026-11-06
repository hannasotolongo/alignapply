package jobs

import "testing"

func TestSplitWorkdayJobID(t *testing.T) {
	board := Board{ATS: "workday", Slug: "wawa.wd1.myworkdayjobs.com/wawa/careers"}
	id := crawledJobID(board, "/job/Store-993---Manalapan-NJ-07726/Customer-Service-Associate_JR117128")
	slug, path, ok := splitWorkdayJobID(id)
	if !ok || slug != board.Slug || path != "/job/Store-993---Manalapan-NJ-07726/Customer-Service-Associate_JR117128" {
		t.Fatalf("got %q %q %v", slug, path, ok)
	}
	if _, _, ok := splitWorkdayJobID("greenhouse:bayada:123"); ok {
		t.Fatal("not a Workday job")
	}
	job := workdayListingJob(board, "wawa.wd1.myworkdayjobs.com", "careers", "/job/x/y_1", "Customer Service Associate", "Store 993 - Manalapan, NJ 07726")
	if job.ID != crawledJobID(board, "/job/x/y_1") || job.ApplyURL != "https://wawa.wd1.myworkdayjobs.com/careers/job/x/y_1" || job.Description != "" {
		t.Fatalf("listing job: %+v", job)
	}
}
