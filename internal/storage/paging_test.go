package storage

import (
	"math"
	"testing"
)

func TestNormalizePageAndRequestPage(t *testing.T) {
	tests := []struct {
		name                   string
		page, pageSize         int
		wantPage, wantPageSize int
		wantRequestSize        int
	}{
		{"defaults", 0, 0, 1, DefaultPageSize, DefaultPageSize},
		{"negative", -3, -7, 1, DefaultPageSize, DefaultPageSize},
		{"kept", 3, 20, 3, 20, 20},
		{"only requests are capped", 1, 5000, 1, 5000, MaxPageSize},
		{"huge page kept", math.MaxInt, 4, math.MaxInt, 4, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, s := NormalizePage(tt.page, tt.pageSize)
			if p != tt.wantPage || s != tt.wantPageSize {
				t.Errorf("NormalizePage(%d, %d) = %d/%d, want %d/%d", tt.page, tt.pageSize, p, s, tt.wantPage, tt.wantPageSize)
			}
			p, s = RequestPage(tt.page, tt.pageSize)
			if p != tt.wantPage || s != tt.wantRequestSize {
				t.Errorf("RequestPage(%d, %d) = %d/%d, want %d/%d", tt.page, tt.pageSize, p, s, tt.wantPage, tt.wantRequestSize)
			}
		})
	}
}

func TestPageOffset(t *testing.T) {
	tests := []struct {
		name                  string
		page, pageSize, total int
		want                  int
	}{
		{"first page", 1, 10, 25, 0},
		{"second page", 2, 10, 25, 10},
		{"last partial page", 3, 10, 25, 20},
		{"page just past the end", 4, 10, 25, 25},
		{"exact boundary", 3, 10, 30, 20},
		{"page at MaxInt", math.MaxInt, 10, 25, 25},
		{"page past MaxInt/pageSize", math.MaxInt/200 + 2, 200, 25, 25},
		{"empty list", 1, 10, 0, 0},
		{"empty list huge page", math.MaxInt, 10, 0, 0},
		{"page below one", 0, 10, 25, 0},
		{"page size below one uses the default", 2, 0, 25, 25},
		{"page size below one on a long list", 2, 0, 120, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PageOffset(tt.page, tt.pageSize, tt.total)
			if got != tt.want {
				t.Errorf("PageOffset(%d, %d, %d) = %d, want %d", tt.page, tt.pageSize, tt.total, got, tt.want)
			}
			if got < 0 || got > tt.total {
				t.Errorf("PageOffset(%d, %d, %d) = %d, outside [0, %d]", tt.page, tt.pageSize, tt.total, got, tt.total)
			}
		})
	}
}

func TestPageSliceHugePageIsEmpty(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	for _, page := range []int{math.MaxInt, math.MaxInt/200 + 2} {
		if window := PageSlice(items, page, 2); len(window) != 0 {
			t.Errorf("PageSlice(page=%d) = %v, want empty", page, window)
		}
	}
	window := PageSlice(items, 2, 2)
	if len(window) != 2 || window[0] != 3 {
		t.Errorf("PageSlice(page=2) = %v, want [3 4]", window)
	}
}

func TestPageSliceHugePageSizeDoesNotWrap(t *testing.T) {
	items := []int{1, 2, 3}
	for _, tc := range []struct{ page, pageSize, wantLen int }{
		{1, math.MaxInt, 3},
		{2, math.MaxInt, 0},
		{1, math.MaxInt - 1, 3},
		{2, 2, 1},
	} {
		if got := PageSlice(items, tc.page, tc.pageSize); len(got) != tc.wantLen {
			t.Errorf("PageSlice(page=%d, pageSize=%d) = %v, want %d items", tc.page, tc.pageSize, got, tc.wantLen)
		}
	}
}
