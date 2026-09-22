package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	maxCareerProfileBodyBytes = 1 << 20 // 1 MiB
	maxHeadlineLength         = 200
	maxSummaryLength          = 5000
	maxTargetRoleLength       = 200
	maxLocationLength         = 200
	maxEvidenceItems          = 500
	maxEvidenceTextLength     = 5000
	maxEvidenceSourceLength   = 200
)

var validCareerProfileEvidenceCategories = map[string]struct{}{
	"skill":         {},
	"experience":    {},
	"education":     {},
	"certification": {},
	"license":       {},
	"project":       {},
	"summary":       {},
}

func decodeCareerProfileRequest(
	r *http.Request,
	request *careerProfileRequest,
) error {
	if r == nil {
		return errors.New("request is required")
	}

	if request == nil {
		return errors.New("career profile request is required")
	}

	body := http.MaxBytesReader(
		nil,
		r.Body,
		maxCareerProfileBodyBytes,
	)

	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(request); err != nil {
		return fmt.Errorf(
			"decode career profile request: %w",
			err,
		)
	}

	var extra any

	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New(
				"request body must contain exactly one JSON object",
			)
		}

		return fmt.Errorf(
			"decode trailing request data: %w",
			err,
		)
	}

	return nil
}

func validateCareerProfileRequest(
	request careerProfileRequest,
) error {
	if len(request.Headline) > maxHeadlineLength {
		return fmt.Errorf(
			"headline must be %d characters or fewer",
			maxHeadlineLength,
		)
	}

	if len(request.Summary) > maxSummaryLength {
		return fmt.Errorf(
			"summary must be %d characters or fewer",
			maxSummaryLength,
		)
	}

	if len(request.TargetRole) > maxTargetRoleLength {
		return fmt.Errorf(
			"targetRole must be %d characters or fewer",
			maxTargetRoleLength,
		)
	}

	if len(request.Location) > maxLocationLength {
		return fmt.Errorf(
			"location must be %d characters or fewer",
			maxLocationLength,
		)
	}

	if len(request.Evidence) > maxEvidenceItems {
		return fmt.Errorf(
			"evidence must contain %d items or fewer",
			maxEvidenceItems,
		)
	}

	seen := make(
		map[string]struct{},
		len(request.Evidence),
	)

	for index, item := range request.Evidence {
		if _, ok := validCareerProfileEvidenceCategories[item.Category]; !ok {
			return fmt.Errorf(
				"evidence[%d].category is invalid",
				index,
			)
		}

		if item.EntryIndex < 0 {
			return fmt.Errorf(
				"evidence[%d].entryIndex must be zero or greater",
				index,
			)
		}

		if item.Text == "" {
			return fmt.Errorf(
				"evidence[%d].text is required",
				index,
			)
		}

		if len(item.Text) > maxEvidenceTextLength {
			return fmt.Errorf(
				"evidence[%d].text must be %d characters or fewer",
				index,
				maxEvidenceTextLength,
			)
		}

		if item.Source == "" {
			return fmt.Errorf(
				"evidence[%d].source is required",
				index,
			)
		}

		if len(item.Source) > maxEvidenceSourceLength {
			return fmt.Errorf(
				"evidence[%d].source must be %d characters or fewer",
				index,
				maxEvidenceSourceLength,
			)
		}

		key := strings.Join(
			[]string{
				item.Category,
				fmt.Sprintf("%d", item.EntryIndex),
				item.Text,
			},
			"\x00",
		)

		if _, exists := seen[key]; exists {
			return fmt.Errorf(
				"evidence[%d] duplicates an existing evidence item",
				index,
			)
		}

		seen[key] = struct{}{}
	}

	return nil
}
