package storage

// Shared paging rules for every list: page is 1-based and defaults to 1,
// page_size defaults to DefaultPageSize. Client-supplied sizes are capped at
// MaxPageSize by RequestPage; internal callers that walk a whole list with a
// larger size go through NormalizePage and are not capped.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// NormalizePage applies the shared defaults to a page and page size.
func NormalizePage(page, pageSize int) (p, size int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	return page, pageSize
}

// RequestPage normalizes a client-supplied page and page_size and caps the
// size at MaxPageSize. Handlers pass the result to the store and echo it.
func RequestPage(page, pageSize int) (p, size int) {
	page, pageSize = NormalizePage(page, pageSize)
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return page, pageSize
}

// PageOffset returns the 0-based offset of a 1-based page in a list of total
// items. A page past the end, including one whose (page-1)*pageSize would
// wrap around MaxInt, maps to total so the caller yields an empty page rather
// than a negative slice bound or a negative SQL OFFSET. The check compares by
// division so the product is only formed when it fits.
func PageOffset(page, pageSize, total int) int {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}
	if page-1 > total/pageSize {
		return total
	}
	return (page - 1) * pageSize
}

// PageSlice selects the 1-based page of items after NormalizePage. A page
// past the end is empty rather than an error, so a client can walk pages
// until items run out.
func PageSlice[T any](items []T, page, pageSize int) []T {
	page, pageSize = NormalizePage(page, pageSize)
	start := PageOffset(page, pageSize, len(items))
	return items[start:min(start+pageSize, len(items))]
}
