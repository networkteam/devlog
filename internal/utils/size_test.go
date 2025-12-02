// Tests adapted from github.com/DmitriyVTitov/size
// Original source: https://github.com/DmitriyVTitov/size/blob/master/size_test.go
package utils

import (
	"testing"
)

func TestSizeOf(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want int
	}{
		{
			name: "Array",
			v:    [3]int32{1, 2, 3}, // 3 * 4  = 12
			want: 12,
		},
		{
			name: "Slice",
			v:    make([]int64, 2, 5), // 5 * 8 + 24 = 64
			want: 64,
		},
		{
			name: "String",
			v:    "ABCdef", // 6 + 16 = 22
			want: 22,
		},
		{
			name: "Map",
			// (8 + 3 + 16) + (8 + 4 + 16) = 55
			// 55 + 8 + 10.79 * 2 = 84
			v:    map[int64]string{0: "ABC", 1: "DEFG"},
			want: 84,
		},
		{
			name: "Struct",
			v: struct {
				slice     []int64
				array     [2]bool
				structure struct {
					i int8
					s string
				}
			}{
				slice: []int64{12345, 67890}, // 2 * 8 + 24 = 40
				array: [2]bool{true, false},  // 2 * 1 = 2
				structure: struct {
					i int8
					s string
				}{
					i: 5,     // 1
					s: "abc", // 3 * 1 + 16 = 19
				}, // 20 + 7 (padding) = 27
			}, // 40 + 2 + 27 = 69 + 6 (padding) = 75
			want: 75,
		},
		{
			name: "Nil",
			v:    nil,
			want: 0,
		},
		{
			name: "Int64",
			v:    int64(42),
			want: 8,
		},
		{
			name: "Float64",
			v:    float64(3.14),
			want: 8,
		},
		{
			name: "Bool",
			v:    true,
			want: 1,
		},
		{
			name: "ByteSlice",
			v:    []byte("hello world"), // 11 + 24 = 35
			want: 35,
		},
		{
			name: "EmptyString",
			v:    "",
			want: 16, // just the string header
		},
		{
			name: "LargeByteSlice",
			v:    make([]byte, 1000), // 1000 + 24 = 1024
			want: 1024,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SizeOf(tt.v); got != tt.want {
				t.Errorf("SizeOf() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSizeOf_Pointer(t *testing.T) {
	s := "test"
	ptr := &s

	// SizeOf uses reflect.Indirect at the top level, so it measures
	// the dereferenced value: 4 (string data) + 16 (string header) = 20
	got := SizeOf(ptr)
	want := 20
	if got != want {
		t.Errorf("SizeOf(ptr) = %v, want %v", got, want)
	}
}

func TestSizeOf_CircularReference(t *testing.T) {
	type Node struct {
		Value int
		Next  *Node
	}

	// Create a circular reference
	a := &Node{Value: 1}
	b := &Node{Value: 2}
	a.Next = b
	b.Next = a

	// Should not panic or infinite loop
	got := SizeOf(a)
	if got < 0 {
		t.Errorf("SizeOf() returned error for circular reference: %v", got)
	}
}
