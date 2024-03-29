package response

// OK creates a single item response
func OK[T any](data T) SingleItem[T] {
	return SingleItem[T]{Data: data}
}

// OKWithPagination creates a paginated response
func OKWithPagination[T any](data T, pageSize, pageNumber, totalRecords int64, defaultLimit ...int64) *Paginated[T] {
	limit := getDefaultLimit(defaultLimit)

	if pageSize == 0 {
		pageSize = limit
	}

	totalPage := calculateTotalPage(totalRecords, pageSize)

	if pageNumber == 0 {
		pageNumber = 1
	}

	return &Paginated[T]{
		Pagination: Pagination{
			CurrentPage: pageNumber,
			TotalPage:   totalPage,
			TotalRecord: totalRecords,
			Limit:       pageSize,
		},
		Data: data,
	}
}

// SingleItem represents a single data item in the response payload
type SingleItem[T any] struct {
	Data T `json:"data"`
}

// Paginated represents a paginated data set in the response payload
type Paginated[T any] struct {
	Pagination Pagination `json:"pagination"`
	Data       T          `json:"data"`
}

// Pagination contains metadata about the paginated data
type Pagination struct {
	CurrentPage int64 `json:"currentPage"`
	TotalPage   int64 `json:"totalPage"`
	TotalRecord int64 `json:"totalRecord"`
	Limit       int64 `json:"limit"`
}

// getDefaultLimit returns the default limit based on provided options
func getDefaultLimit(defaultLimits []int64) int64 {
	if len(defaultLimits) > 0 {
		return defaultLimits[0]
	}
	return 100
}

// calculateTotalPage calculates the total number of pages based on total records and page size
func calculateTotalPage(totalRecords, pageSize int64) int64 {
	if pageSize == 0 {
		return 1
	}

	totalPage := totalRecords / pageSize
	if totalRecords%pageSize != 0 {
		totalPage++
	}
	return totalPage
}
