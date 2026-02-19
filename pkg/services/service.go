package services

// Pagination is shared across services
type Pagination struct {
	Page     int
	PageSize int
}

// PaginatedResponse is a generic paginated response
type PaginatedResponse[T any] struct {
	Data       []T `json:"data"`
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalCount int `json:"total_count"`
}
