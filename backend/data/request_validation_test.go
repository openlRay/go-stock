package data

import "testing"

func TestNormalizeTHSNumericCode(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		valid bool
	}{
		{name: "concept code", input: " 309269 ", want: "309269", valid: true},
		{name: "industry code", input: "881121", want: "881121", valid: true},
		{name: "path injection", input: "../309269", want: "../309269", valid: false},
		{name: "query injection", input: "309269?x=1", want: "309269?x=1", valid: false},
		{name: "empty", input: "", want: "", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, valid := normalizeTHSNumericCode(tt.input)
			if got != tt.want || valid != tt.valid {
				t.Fatalf("normalizeTHSNumericCode(%q) = (%q, %v), want (%q, %v)", tt.input, got, valid, tt.want, tt.valid)
			}
		})
	}
}

func TestNormalizeISODate(t *testing.T) {
	tests := []struct {
		input string
		want  string
		valid bool
	}{
		{input: "", want: "", valid: true},
		{input: " 2026-08-10 ", want: "2026-08-10", valid: true},
		{input: "2026-02-30", want: "2026-02-30", valid: false},
		{input: "2026-08-10&sortType=asc", want: "2026-08-10&sortType=asc", valid: false},
	}

	for _, tt := range tests {
		got, valid := normalizeISODate(tt.input)
		if got != tt.want || valid != tt.valid {
			t.Fatalf("normalizeISODate(%q) = (%q, %v), want (%q, %v)", tt.input, got, valid, tt.want, tt.valid)
		}
	}
}

func TestRzrqRankRejectsInvalidRequestParameters(t *testing.T) {
	api := NewMarketNewsApi()
	tests := []struct {
		name     string
		typeName string
		sortKey  string
		sortType string
		date     string
		offset   int
	}{
		{name: "type", typeName: "../../admin", sortKey: "jmr", sortType: "desc"},
		{name: "sort key", typeName: "hyList", sortKey: "jmr&offset=999", sortType: "desc"},
		{name: "sort type", typeName: "hyList", sortKey: "jmr", sortType: "desc&x=1"},
		{name: "date", typeName: "hyList", sortKey: "jmr", sortType: "desc", date: "2026-08-10&x=1"},
		{name: "offset", typeName: "hyList", sortKey: "jmr", sortType: "desc", offset: 10001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := api.RzrqRank(tt.typeName, tt.sortKey, tt.sortType, tt.date, 20, tt.offset)
			if result == nil || len(result.List) != 0 {
				t.Fatalf("invalid request returned data: %+v", result)
			}
		})
	}
}

func TestRzrqTrendRejectsUnsupportedScopedRequest(t *testing.T) {
	api := NewMarketNewsApi()
	tests := []struct {
		name     string
		typeName string
		code     string
	}{
		{name: "industry", typeName: "hyList", code: "881121"},
		{name: "concept", typeName: "gnList", code: "309269"},
		{name: "stock", typeName: "ggList", code: "600000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := api.RzrqTrend(tt.typeName, tt.code)
			if result == nil || len(result.Items) != 0 {
				t.Fatalf("unsupported scoped request returned data: %+v", result)
			}
		})
	}
}
