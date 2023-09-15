package response_test

import (
	"reflect"
	"testing"

	. "github.com/velmie/x/svc/http/response"
)

func TestOKWithPagination(t *testing.T) {
	tests := []struct {
		name         string
		data         any
		pageSize     int64
		pageNumber   int64
		totalRecords int64
		defaultLimit []int64
		want         *Paginated[any]
	}{
		{
			name:         "Using default values",
			data:         []int{1, 2, 3},
			pageSize:     0,
			pageNumber:   0,
			totalRecords: 100,
			defaultLimit: []int64{},
			want: &Paginated[any]{
				Pagination: Pagination{
					CurrentPage: 1,
					TotalPage:   1,
					TotalRecord: 100,
					Limit:       100,
				},
				Data: []int{1, 2, 3},
			},
		},
		{
			name:         "Custom Limit",
			data:         []int{1, 2, 3},
			pageSize:     50,
			pageNumber:   2,
			totalRecords: 100,
			defaultLimit: []int64{200},
			want: &Paginated[any]{
				Pagination: Pagination{
					CurrentPage: 2,
					TotalPage:   2,
					TotalRecord: 100,
					Limit:       50,
				},
				Data: []int{1, 2, 3},
			},
		},
		{
			name:         "PageSize Greater than TotalRecords",
			data:         []int{4, 5, 6},
			pageSize:     150,
			pageNumber:   1,
			totalRecords: 100,
			defaultLimit: []int64{},
			want: &Paginated[any]{
				Pagination: Pagination{
					CurrentPage: 1,
					TotalPage:   1,
					TotalRecord: 100,
					Limit:       150,
				},
				Data: []int{4, 5, 6},
			},
		},
		{
			name:         "TotalRecords is a Multiple of PageSize",
			data:         []int{7, 8, 9},
			pageSize:     50,
			pageNumber:   1,
			totalRecords: 100,
			defaultLimit: []int64{},
			want: &Paginated[any]{
				Pagination: Pagination{
					CurrentPage: 1,
					TotalPage:   2,
					TotalRecord: 100,
					Limit:       50,
				},
				Data: []int{7, 8, 9},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OKWithPagination(tt.data, tt.pageSize, tt.pageNumber, tt.totalRecords, tt.defaultLimit...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("OKWithPagination() = %v, want %v", got, tt.want)
			}
		})
	}
}
