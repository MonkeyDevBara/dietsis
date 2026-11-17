package nutrition

import (
	"encoding/json"
	"os"
	"testing"
)

type vectors struct {
	Targets []struct {
		Name     string  `json:"name"`
		Input    Body    `json:"input"`
		Expected Targets `json:"expected"`
	} `json:"targets"`
	EntryTotals []struct {
		Name       string    `json:"name"`
		PerServing Nutrients `json:"per_serving"`
		Quantity   float64   `json:"quantity"`
		Expected   Nutrients `json:"expected"`
	} `json:"entry_totals"`
	SumTotals []struct {
		Name     string      `json:"name"`
		Items    []Nutrients `json:"items"`
		Expected Nutrients   `json:"expected"`
	} `json:"sum_totals"`
}

func load(t *testing.T) vectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRecommendMatchesSharedVectors(t *testing.T) {
	for _, c := range load(t).Targets {
		t.Run(c.Name, func(t *testing.T) {
			if got := Recommend(c.Input); got != c.Expected {
				t.Fatalf("got %+v, want %+v", got, c.Expected)
			}
		})
	}
}

func TestEntryTotalsMatchSharedVectors(t *testing.T) {
	for _, c := range load(t).EntryTotals {
		t.Run(c.Name, func(t *testing.T) {
			if got := EntryTotals(c.PerServing, c.Quantity); got != c.Expected {
				t.Fatalf("got %+v, want %+v", got, c.Expected)
			}
		})
	}
}

func TestSumTotalsMatchSharedVectors(t *testing.T) {
	for _, c := range load(t).SumTotals {
		t.Run(c.Name, func(t *testing.T) {
			if got := SumTotals(c.Items); got != c.Expected {
				t.Fatalf("got %+v, want %+v", got, c.Expected)
			}
		})
	}
}
