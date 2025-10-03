package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"runtime"
)

// TicketLock 
type TicketLock struct {
	nextTicket uint32 // The next ticket number to be issued
	nowServing uint32 // The ticket number currently being served
}

// Lock acquires the ticket lock in FIFO order.
func (lock_ticket *TicketLock) Lock() {
	myTicket := atomic.AddUint32(&lock_ticket.nextTicket, 1) - 1 // Atomically get a ticket
	for atomic.LoadUint32(&lock_ticket.nowServing) != myTicket { // Spin until it's our turn
		runtime.Gosched() // Yield the processor to allow other goroutines to run
	}
}

func (lock_ticket *TicketLock) Unlock() {
	atomic.AddUint32(&lock_ticket.nowServing, 1) // Unlock releases the lock by incrementing the nowServing counter.
}

// CASLock
type CASLock struct {
	state int32 // 0 = unlocked, 1 = locked
}

// Lock acquires the CAS lock by spinning until the state becomes 0 and sets it to 1 atomically.
func (lock_cas *CASLock) Lock() {
	for !atomic.CompareAndSwapInt32(&lock_cas.state, 0, 1) {
		runtime.Gosched() // Yield to allow other goroutines to proceed
	}
}

// Unlock releases the CAS lock by setting the state to 0.
func (lock_cas *CASLock) Unlock() {
	atomic.StoreInt32(&lock_cas.state, 0)
}

// Lock interface allows benchmarking different locking mechanisms.
type Lock interface {
	Lock()
	Unlock()
}

// benchmark measures the average wait time for a lock across multiple goroutines and iterations.
func benchmark(lock Lock, goroutines int, iterations int) time.Duration {
	var waiting sync.WaitGroup
	waiting.Add(goroutines)

	totalWaitTime := int64(0) // Total wait time accumulated by all goroutines
	var totalWaitTimeMutex sync.Mutex // Mutex to protect access to totalWaitTime

	// Launch multiple goroutines to simulate concurrent access
	for i := 0; i < goroutines; i++ {
		go func() {
			defer waiting.Done()
			for j := 0; j < iterations; j++ {
				start := time.Now()      // Record time before acquiring the lock
				lock.Lock()             // Try to acquire the lock
				waitTime := time.Since(start) // Measure time spent waiting

				// Critical section - simulate work
				time.Sleep(100 * time.Microsecond)

				lock.Unlock()           // Release the lock

				// Accumulate wait time safely
				totalWaitTimeMutex.Lock()
				totalWaitTime += waitTime.Nanoseconds()
				totalWaitTimeMutex.Unlock()
			}
		}()
	}

	waiting.Wait() // Wait for all goroutines to finish

	// Return the average wait time per lock acquisition
	averageWait := time.Duration(totalWaitTime / int64(goroutines*iterations))
	return averageWait
}

// main runs the benchmark for both lock types across varying numbers of goroutines.
func main() {
	const iterations = 1000 // Number of lock acquisitions per goroutine
	threadCounts := []int{1, 2, 4, 8, 16, 32, 64, 128} // Test with increasing concurrency levels

	fmt.Printf("Goroutines\tTicketLock(ms)\tCASLock(ms)\n")
	for _, threads := range threadCounts {
		ticket := &TicketLock{} // Create a new TicketLock instance
		cas := &CASLock{}       // Create a new CASLock instance

		// Benchmark each lock type
		ticketTime := benchmark(ticket, threads, iterations)
		casTime := benchmark(cas, threads, iterations)

		// Output average wait times in milliseconds
		fmt.Printf("%d\t\t%d\t\t%d\n", threads, ticketTime.Nanoseconds()/1000000, casTime.Nanoseconds()/1000000)
	}
}
