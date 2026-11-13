package tailor

import "testing"

func TestPlain(t *testing.T) {
	cases := map[string]string{
		"Spearheaded a migration — leveraging Kubernetes!": "Led a migration, using Kubernetes.",
		"Great question! Utilize the STAR method.":         "Utilize the STAR method.",
		"In today's fast-paced world, recruiters skim.":    "Recruiters skim.",
		"Cut p95 latency from 900ms to 180ms":              "Cut p95 latency from 900ms to 180ms",
	}
	for in, want := range cases {
		got := Plain(in)
		if in == "Great question! Utilize the STAR method." {
			want = "Use the STAR method."
		}
		if got != want {
			t.Errorf("Plain(%q) = %q, want %q", in, got, want)
		}
	}
}
