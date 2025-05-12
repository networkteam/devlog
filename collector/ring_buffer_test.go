package collector_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/networkteam/devlog/collector"
)

func TestRingBuffer_Basic(t *testing.T) {
	// Create a new ring buffer with capacity 3
	rb := collector.NewRingBuffer[string](3)

	// Check initial state
	assert.Equal(t, uint64(0), rb.Size())
	assert.Equal(t, uint64(3), rb.Capacity())

	// Add a record
	rb.Add("data1")

	// Check size
	assert.Equal(t, uint64(1), rb.Size())

	// GetRecords should return the record
	records := rb.GetRecords(1)
	assert.Len(t, records, 1)
	assert.Equal(t, "data1", records[0])

	// Add more records
	rb.Add("data2")
	rb.Add("data3")

	// Check size
	assert.Equal(t, uint64(3), rb.Size())

	// GetRecords should return all records in correct order (newest last)
	records = rb.GetRecords(3)
	assert.Len(t, records, 3)
	assert.Equal(t, "data1", records[0])
	assert.Equal(t, "data2", records[1])
	assert.Equal(t, "data3", records[2])
}

func TestRingBuffer_Overwrite(t *testing.T) {
	// Create a new ring buffer with capacity 3
	rb := collector.NewRingBuffer[string](3)

	// Fill the buffer to capacity
	rb.Add("data1")
	rb.Add("data2")
	rb.Add("data3")

	// Buffer is now full
	assert.Equal(t, uint64(3), rb.Size())

	// Add a new record which should overwrite the oldest one
	rb.Add("data4")

	// Size should still be at capacity
	assert.Equal(t, uint64(3), rb.Size())

	// GetRecords should return the records in order (newest last)
	records := rb.GetRecords(3)
	assert.Len(t, records, 3)
	assert.Equal(t, "data2", records[0])
	assert.Equal(t, "data3", records[1])
	assert.Equal(t, "data4", records[2])

	// Add two more records
	rb.Add("data5")
	rb.Add("data6")

	// Check order of records
	records = rb.GetRecords(3)
	assert.Len(t, records, 3)
	assert.Equal(t, "data4", records[0])
	assert.Equal(t, "data5", records[1])
	assert.Equal(t, "data6", records[2])
}

func TestRingBuffer_GetRecords(t *testing.T) {
	// Create a new ring buffer with capacity 5
	rb := collector.NewRingBuffer[string](5)

	// Add records
	rb.Add("data1")
	rb.Add("data2")
	rb.Add("data3")

	// Get fewer records than available
	records := rb.GetRecords(2)
	assert.Len(t, records, 2)
	assert.Equal(t, "data2", records[0])
	assert.Equal(t, "data3", records[1])

	// Get more records than available
	records = rb.GetRecords(10)
	assert.Len(t, records, 3)
	assert.Equal(t, "data1", records[0])
	assert.Equal(t, "data2", records[1])
	assert.Equal(t, "data3", records[2])

	// Fill the buffer beyond capacity
	rb.Add("data4")
	rb.Add("data5")
	rb.Add("data6")

	// Check that we get correct records (the latest ones)
	records = rb.GetRecords(5)
	assert.Len(t, records, 5)
	assert.Equal(t, "data2", records[0])
	assert.Equal(t, "data3", records[1])
	assert.Equal(t, "data4", records[2])
	assert.Equal(t, "data5", records[3])
	assert.Equal(t, "data6", records[4])

	// Get fewer records than available
	records = rb.GetRecords(3)
	assert.Len(t, records, 3)
	assert.Equal(t, "data4", records[0])
	assert.Equal(t, "data5", records[1])
	assert.Equal(t, "data6", records[2])
}

func TestRingBuffer_EmptyBuffer(t *testing.T) {
	// Create a new ring buffer with capacity 3
	rb := collector.NewRingBuffer[string](3)

	// GetRecords on empty buffer should return empty slice
	records := rb.GetRecords(5)
	assert.Empty(t, records)
}

func TestRingBuffer_DifferentTypes(t *testing.T) {
	// Test with integer type
	rbInt := collector.NewRingBuffer[int](3)
	rbInt.Add(1)
	rbInt.Add(2)
	rbInt.Add(3)

	records := rbInt.GetRecords(3)
	assert.Equal(t, 1, records[0])
	assert.Equal(t, 2, records[1])
	assert.Equal(t, 3, records[2])

	// Test with struct type
	type Person struct {
		Name string
		Age  int
	}

	rbPerson := collector.NewRingBuffer[Person](2)
	rbPerson.Add(Person{Name: "Alice", Age: 30})
	rbPerson.Add(Person{Name: "Bob", Age: 25})

	people := rbPerson.GetRecords(2)
	assert.Equal(t, "Alice", people[0].Name)
	assert.Equal(t, "Bob", people[1].Name)
}

func TestRingBuffer_ZeroCapacity(t *testing.T) {
	// Creating a ring buffer with capacity 0 should panic
	assert.Panics(t, func() {
		collector.NewRingBuffer[string](0)
	})
}

func TestRingBuffer_LargeCapacity(t *testing.T) {
	// Create a buffer with large capacity
	capacity := uint64(1000)
	rb := collector.NewRingBuffer[int](capacity)

	// Add many records
	for i := 0; i < 2000; i++ {
		rb.Add(i)
	}

	// Check size
	assert.Equal(t, capacity, rb.Size())

	// Get the latest 10 records
	records := rb.GetRecords(10)
	assert.Len(t, records, 10)

	// Verify the records are the most recent ones
	for i := 0; i < 10; i++ {
		assert.Equal(t, 1990+i, records[i])
	}
}

func TestRingBuffer_GetZeroRecords(t *testing.T) {
	// Create a new ring buffer with capacity 3
	rb := collector.NewRingBuffer[string](3)

	// Add some records
	rb.Add("data1")
	rb.Add("data2")

	// Request zero records
	records := rb.GetRecords(0)
	assert.Empty(t, records)
}

func TestRingBuffer_Wraparound(t *testing.T) {
	// Create a new ring buffer with capacity 3
	rb := collector.NewRingBuffer[string](3)

	// Add initial records
	rb.Add("data1")
	rb.Add("data2")
	rb.Add("data3")

	// Add more records to cause wraparound
	rb.Add("data4")
	rb.Add("data5")

	// Check that the buffer wrapped around correctly
	records := rb.GetRecords(3)
	assert.Len(t, records, 3)
	assert.Equal(t, "data3", records[0])
	assert.Equal(t, "data4", records[1])
	assert.Equal(t, "data5", records[2])

	// Add more records
	rb.Add("data6")
	rb.Add("data7")
	rb.Add("data8")

	// Get all records
	records = rb.GetRecords(3)
	assert.Len(t, records, 3)
	assert.Equal(t, "data6", records[0])
	assert.Equal(t, "data7", records[1])
	assert.Equal(t, "data8", records[2])
}
