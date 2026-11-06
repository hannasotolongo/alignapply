// Command janestoredcheck reads one Workday board, keeps only the jobs saved
// from its list (no details yet), and runs a search through the saved-jobs
// provider to check that it reads and scores them on the spot. Dev tool only.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
	"github.com/hannasotolongo/casemade-backend/internal/matching"
)

type memoryStore struct{ jobs []jobs.Job }

func (m *memoryStore) SeedBoards(context.Context, []jobs.Board) error { return nil }
func (m *memoryStore) DueBoards(context.Context, int, time.Duration) ([]jobs.Board, error) {
	return nil, nil
}
func (m *memoryStore) KnownJobIDs(context.Context, jobs.Board) (map[string]bool, error) {
	return nil, nil
}
func (m *memoryStore) SaveBoardJobs(context.Context, jobs.Board, []jobs.Job, []string, bool) error {
	return nil
}
func (m *memoryStore) RecordCrawl(context.Context, jobs.Board, string, int, bool) error { return nil }
func (m *memoryStore) SearchJobs(context.Context, string, string, int) ([]jobs.Job, error) {
	return m.jobs, nil
}
func (m *memoryStore) SaveJobDetails(context.Context, []jobs.Job) error { return nil }
func (m *memoryStore) PruneClosed(context.Context, time.Duration) error { return nil }

func main() {
	board := jobs.Board{ATS: "workday", Slug: "wawa.wd1.myworkdayjobs.com/wawa/careers", Company: "Wawa"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	found, err := jobs.CheckBoardListOnly(ctx, board)
	fmt.Println("list read; waiting 20s before searching")
	time.Sleep(20 * time.Second)
	if err != nil {
		panic(err)
	}
	store := &memoryStore{}
	for _, job := range found {
		if job.Description == "" {
			job.Source = "workday"
			job.IsActive = true
			store.jobs = append(store.jobs, job)
		}
	}
	fmt.Printf("store holds %d jobs saved from the list only\n", len(store.jobs))
	provider := jobs.NewStoredJobsProvider(func() jobs.BoardStore { return store })
	pipeline := jobs.NewPipeline()
	for _, search := range [][2]string{{"Customer Service Associate", "Orlando, FL"}, {"Team Supervisor", "Philadelphia, PA"}} {
		request := jobs.SearchRequest{TargetRole: search[0], Location: search[1], CountryCode: "US", ResumeText: "Customer service associate at a convenience store for 2 years. Cash handling, food prep, stocking, teamwork."}
		started := time.Now()
		result, err := provider.Search(context.Background(), request)
		kept := pipeline.Process(request, result)
		scored := 0
		for _, job := range kept {
			if m := matching.MatchFast(request, job); m.MatchLevel != "Insufficient Evidence" {
				scored++
			}
		}
		fmt.Printf("\n### %s in %s: read %d on the spot in %s, %d shown after filters, %d scored (err=%v)\n", search[0], search[1], len(result), time.Since(started).Round(100*time.Millisecond), len(kept), scored, err)
		for i, job := range kept {
			if i == 4 {
				break
			}
			m := matching.MatchFast(request, job)
			miles := "?"
			if job.DistanceMiles != nil {
				miles = fmt.Sprintf("%.0f mi", *job.DistanceMiles)
			}
			fmt.Printf("  - %d%% %s | %s (%s) | desc %d chars\n", m.MatchPercentage, job.Title, job.Location, miles, len(job.Description))
		}
	}
}
