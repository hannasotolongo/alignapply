// Command janereadcheck reads one board per new hiring system from a
// discovery file and prints a sample, to check the readers. Dev tool only.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
)

func main() {
	var boards []jobs.Board
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(data, &boards); err != nil {
		panic(err)
	}
	checked := map[string]int{}
	for _, board := range boards {
		if checked[board.ATS] >= 2 {
			continue
		}
		checked[board.ATS]++
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		found, complete, err := jobs.CheckBoard(ctx, board)
		cancel()
		described := 0
		for _, job := range found {
			if job.Description != "" {
				described++
			}
		}
		fmt.Printf("\n### %s %s (%s): %d jobs (%d with details, %d from the list), complete=%v, err=%v\n", board.ATS, board.Slug, board.Company, len(found), described, len(found)-described, complete, err)
		for i, job := range found {
			if i == 3 {
				break
			}
			fmt.Printf("  - %s | %s | country=%s | desc=%d chars | %s\n", job.Title, job.Location, job.CountryCode, len(job.Description), job.ApplyURL)
		}
	}
}
