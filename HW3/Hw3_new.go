package main

import (
	"fmt"
	"sync"
	"math/rand"
	"time"
)

// Node represents a single node in the linked list with a value, 
// a pointer to the next node, and a mutex for thread-safe access.
type Node struct {
	value int			//the data being held in the node
	next  *Node			//node used to point to the next value in the list
	lock  sync.Mutex	//lock is used as a blocker to keep information from being shared between thread and allows only one thread to use data at a time as long as the lock is used correctly
}

// hand-over-hand lock coupling technique.
type HoHList struct {
	head *Node
}

type SimpleNode struct {
	key  int					//data being held in the node (like the value variable in the Node struct)
	next *SimpleNode
}

type SimpleList struct {		
	head *SimpleNode			//Pointer to the head (first node) of the list
	lock sync.Mutex				//same as in the node struct
}

func NewSimpleList() *SimpleList {		// NewSimpleList creates and returns a new, empty SimpleList.
	return &SimpleList{}				// Initialize SimpleList with nil head and zero-value mutex
}


func (l *SimpleList) Add(key int) bool {		// Add inserts a new node with the given key at the head of the list.
	newNode := &SimpleNode{key: key}			// Create a new node with the given key

	l.lock.Lock()								// Acquire lock before modifying the list
	newNode.next = l.head						// Point new node's next to current head
	l.head = newNode							// Update head to the new node
	l.lock.Unlock()								// unlocks

	return true 								// always succeeds
}

func (l *SimpleList) Remove(key int) bool {		//Remove searches the list for the first occurrence of the given key and removes it.
	l.lock.Lock()								// Lock the list to ensure exclusive access
	defer l.lock.Unlock()						// Defer unlocking until function exit

	var prev *SimpleNode						// Previous node pointer, starts nil
	curr := l.head								//Start from the head node

	for curr != nil {
		if curr.key == key {					// Found the node to remove
			if prev == nil {					
				l.head = curr.next				// Removing the head node, Update head to next node
			} else {
				prev.next = curr.next			// Removing a node beyond the head, Link previous node to node after current
			}
			return true							// Successfully removed node
		}
		prev = curr								// Move previous pointer forward
		curr = curr.next						// Move current pointer forward
	}
	return false								// Key not found in the list
}

func (l *SimpleList) Contains(key int) bool {	// Contains returns true if key found.
	l.lock.Lock()								// Lock the list for safe concurrent access
	defer l.lock.Unlock()						// Unlock after the search finishes

	curr := l.head								// Start from the head node
	for curr != nil {		
		if curr.key == key {
			return true							// Key found
		}
		curr = curr.next						// Move to next node
	}
	return false								// Key not found
}


// HoHList Methods


func NewHoHList() *HoHList {			// NewHoHList initializes a FineGrainedList with intial head and final tail nodes.
	head := &Node{value: -1}			// intial head node
	tail := &Node{value: 1<<31 - 1}		// final tail node (max int value)
	head.next = tail
	return &HoHList{head: head}			// Return new list with initialized head
}

func (l *HoHList) Add(val int) bool {		// Add inserts a value into the list Using hand-over-hand locking.
	previous := l.head						//sets the previous node to the head
	previous.lock.Lock()					//locks the previous node
	current := previous.next				//sets current node equal to the next item that previous points to
	current.lock.Lock()						//locks the current node

	for current.value < val {			//goes through the list as long as it is smaller than val
		previous.lock.Unlock()			//unlocks the previous node
		previous = current				//sets previous equal to current essentially 
		current = current.next			//sets the current node to the next value in the list the current node points to
		current.lock.Lock()				//locks the current node
	}

	if current.value == val {		//checks if Value already exists
		current.lock.Unlock()		//if the value is equal it unlocks everything 
		previous.lock.Unlock()
		return false				//returns false if there is the same value in the list already
	}

	newNode := &Node{value: val, next: current}		//sets newNode to be point towards the current node
	previous.next = newNode							//sets the previous node pointing towards newNode
	current.lock.Unlock()							//unlocks both
	previous.lock.Unlock()
	return true										//returns true since the data got added
}


func (l *HoHList) Remove(val int) bool {		// Remove deletes a node, Uses fine-grained locking.
	previous := l.head							//Same as add function
	previous.lock.Lock()
	current := previous.next
	current.lock.Lock()

	for current.value < val {					//goes through the list as long as it is smaller than val, same as add function as well
		previous.lock.Unlock()					//same as add function
		previous = current
		current = current.next
		current.lock.Lock()
	}

	if current.value != val {					//if the value isnt found, unlock both previous and current nodes
		current.lock.Unlock()
		previous.lock.Unlock()
		return false							//Returns false since the value couldn't be located to be removed
	}

	previous.next = current.next				//sets the previous next node equal to the currents next node essentially skipping one spot which is the value we removed
	current.lock.Unlock()						//unlocks both
	previous.lock.Unlock()
	return true									//returns true after removing the value
}

// Contains checks if a value is in the list.
// Uses hand-over-hand locking.
func (l *HoHList) Contains(val int) bool {		// Contains checks whether a value is  i the list, Uses HoH locking
	previous := l.head							//same as both add and remove functions
	previous.lock.Lock()
	current := previous.next
	current.lock.Lock()

	// Traverse the list
	for current.value < val {					//same as both add and remove functions
		previous.lock.Unlock()
		previous = current
		current = current.next
		current.lock.Lock()
	}

	found := current.value == val				//checks if current.value is equal to the value being input into th function, then sets it equal to found
	current.lock.Unlock()
	previous.lock.Unlock()
	return found								//returns the found value
}

// Benchmarking

const (
	numThreads = 8      // Number of concurrent goroutines
	operations = 10000  // Operations per goroutine
	maxValue   = 1000   // Max value to use in list
)

// simulateBenchmark runs a mix of Add, Remove, and Contains operations
// on the given list using multiple goroutines to simulate load.
func simulateBenchmark(list interface {
	Add(int) bool
	Remove(int) bool 
	Contains(int) bool
	}, readPercent, writePercent int, name string) {
	start := time.Now()		//starts the timer for the benchmarking
	var wg sync.WaitGroup

	// Spawn multiple goroutines for parallel operations
	for t := 0; t < numThreads; t++ {
		wg.Add(1) //says to wait for one more goroutine to be added (adds 1 to the counter)
		go func() {
			defer wg.Done()												//when the goroutine ends calles wg.Done(), Done() signals that a goroutine has finished (subtracts 1 form the counter)
			for j := 0; j < operations; j++ {							//loops the tests so there can be a variety of tests over multiple itarations.
				val := rand.Intn(maxValue)								//assigns a random variable to be put into the lists
				choice := rand.Intn(100)								//assigns a random variable out of 100 to choose the type of test in comparison with the read, or write percent variable.
				if choice < readPercent {								//if choise is less than readPercent run contains function
					list.Contains(val)
				} else if choice < readPercent+(writePercent/2) {		//if choice is less than the combination of read and write percents call add function (the writePercent/2 is because the theres 2 processes Add and Remove)
					list.Add(val)
				} else {												//If choice is greater than everything call remove function
					list.Remove(val)
				}														//the whole process of changing the functions of the list randomly is
			}															//used to test the different capabilities of the lists functions and how long it takes the data to be updated and finish
		}()
	}

	wg.Wait()												//waits till goroutines are done
	elapsed := time.Since(start) 							//gets the time for each test 
	fmt.Printf("[%s] Elapsed time: %s\n", name, elapsed) 	//prints the times
}

// Main Function

func main() {
	fmt.Println("Starting manual benchmarks...")

	// Benchmark Simple mutex list (replacement for FineGrainedList)
	simpleList := NewSimpleList()
	simulateBenchmark(simpleList, 90, 10, "SimpleList_ReadHeavy")
	simulateBenchmark(simpleList, 0, 100, "SimpleList_WriteHeavy")
	simulateBenchmark(simpleList, 40, 60, "SimpleList_Mixed")

	// Benchmark hand-over-hand list
	hohList := NewHoHList()
	simulateBenchmark(hohList, 90, 10, "HoH_ReadHeavy")
	simulateBenchmark(hohList, 0, 100, "HoH_WriteHeavy")
	simulateBenchmark(hohList, 40, 60, "HoH_Mixed")
}
