package storage

// SearchPage selects the 1-based page of items that a search response
// returns. Page defaults to 1, pageSize to 50 and is capped at 200. A page
// past the end is empty rather than an error, so a client can walk pages
// until items run out.
func SearchPage[T any](items []T, page, pageSize int) (window []T, p, size int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	total := len(items)
	start := total
	// Compare by division so (page-1)*pageSize cannot wrap for a page near MaxInt.
	if page-1 <= total/pageSize {
		start = (page - 1) * pageSize
	}
	end := min(start+pageSize, total)
	return items[start:end], page, pageSize
}
