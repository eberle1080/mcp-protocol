package syncmap

import (
	"sync"
	"testing"
)

// TestConcurrentRangeAndPut verifies that Range and Put can be called concurrently
// without data races or panics.
func TestConcurrentRangeAndPut(t *testing.T) {
	m := NewMap[string, int]()

	// Prepopulate with some data
	for i := 0; i < 10; i++ {
		m.Put(string(rune('a'+i)), i)
	}

	var wg sync.WaitGroup
	done := make(chan bool)

	// Goroutine 1: Continuously add items
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-done:
				return
			default:
				m.Put(string(rune('A'+i%26)), i)
			}
		}
	}()

	// Goroutine 2: Continuously iterate
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-done:
				return
			default:
				count := 0
				m.Range(func(k string, v int) bool {
					count++
					return true
				})
			}
		}
	}()

	// Goroutine 3: Continuously delete items
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-done:
				return
			default:
				m.Delete(string(rune('a' + i%10)))
			}
		}
	}()

	// Goroutine 4: Continuously read Size
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-done:
				return
			default:
				_ = m.Size()
			}
		}
	}()

	wg.Wait()
	close(done)
}

// TestRangeEarlyExit verifies that Range stops when the callback returns false.
func TestRangeEarlyExit(t *testing.T) {
	m := NewMap[string, int]()

	// Add some items
	for i := 0; i < 10; i++ {
		m.Put(string(rune('a'+i)), i)
	}

	count := 0
	m.Range(func(k string, v int) bool {
		count++
		return count < 5 // Stop after 5 items
	})

	if count != 5 {
		t.Errorf("Expected to stop after 5 items, but counted %d", count)
	}
}

// TestRangeWithEmptyMap verifies that Range works correctly with an empty map.
func TestRangeWithEmptyMap(t *testing.T) {
	m := NewMap[string, int]()

	count := 0
	m.Range(func(k string, v int) bool {
		count++
		return true
	})

	if count != 0 {
		t.Errorf("Expected 0 items in empty map, but counted %d", count)
	}
}
