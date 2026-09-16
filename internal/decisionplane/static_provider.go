package decisionplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

type StaticProvider struct {
	ID     string
	Scores map[string]float64
}

func (provider StaticProvider) Decide(_ context.Context, request Request) (Decision, error) {
	if provider.ID == "" {
		return Decision{}, errors.New("static provider id is required")
	}
	if len(request.Choices) == 0 {
		return Decision{}, errors.New("decision request has no choices")
	}

	total := 0.0
	probabilities := make([]Probability, 0, len(request.Choices))
	for _, choice := range request.Choices {
		score, ok := provider.Scores[choice.ID]
		if !ok {
			score = 0
		}
		if score < 0 {
			return Decision{}, fmt.Errorf("negative score for choice %q", choice.ID)
		}
		total += score
		probabilities = append(probabilities, Probability{ChoiceID: choice.ID, Probability: score})
	}
	if total <= 0 {
		return Decision{}, errors.New("static provider scores must contain positive mass")
	}
	for index := range probabilities {
		probabilities[index].Probability /= total
	}

	sorted := append([]Probability(nil), probabilities...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Probability == sorted[j].Probability {
			return sorted[i].ChoiceID < sorted[j].ChoiceID
		}
		return sorted[i].Probability > sorted[j].Probability
	})
	return NewDecision(request, provider.ID, sorted[0].ChoiceID, probabilities, sorted[0].Probability), nil
}
