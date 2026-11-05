package jobs

import (
	_ "embed"
	"encoding/json"
	"log"
	"strings"
)

// seed_boards.json lists employer job boards found and verified by the
// board discovery tool (cmd/discoverboards). Each entry is one Board.
//
//go:embed seed_boards.json
var seedBoardsJSON []byte

// CrawlSeedBoards is every board the crawler should read: the discovered
// list plus the employers Jane already searched live.
func CrawlSeedBoards() []Board {
	var boards []Board
	if err := json.Unmarshal(seedBoardsJSON, &boards); err != nil {
		log.Printf("[crawler] seed_boards.json is invalid: %v", err)
	}
	for _, employer := range append(GreenhouseBoards(), DefaultATSEmployers()...) {
		ats := strings.ToLower(strings.TrimSpace(employer.Provider))
		if _, ok := boardReaders[ats]; ok && strings.TrimSpace(employer.Site) != "" {
			boards = append(boards, Board{ATS: ats, Slug: strings.TrimSpace(employer.Site), Company: employer.Name})
		}
	}

	seen := make(map[string]bool, len(boards))
	unique := boards[:0]
	for _, board := range boards {
		if board.ATS == "" || board.Slug == "" || seen[board.key()] {
			continue
		}
		seen[board.key()] = true
		unique = append(unique, board)
	}
	return unique
}
