//HW group 4
//HW4 Part 2

//Each queue method was basically taken from each source and converted to work in vscode
//So they may differ slightly from the source but formatically are the same


#define _POSIX_C_SOURCE 199309L   // Enables timing stuff, time.h does not work without

#include <stdio.h>     
#include <stdlib.h>     
#include <stdbool.h>    
#include <pthread.h>    
#include <stdatomic.h>  
#include <time.h>       
#include <assert.h>     

/* 
   MUTEX-BASED QUEUE
   */

// Define a node in the linked list queue
typedef struct mt_node {
    int value;                  // Data value stored in the node
    struct mt_node *next;       // Pointer to the next node in the queue
} mt_node_t;

// Define the queue structure
typedef struct mt_queue {
    mt_node_t *head;            // Pointer to the dummy head node
    mt_node_t *tail;            // Pointer to the last (tail) node
    pthread_mutex_t head_lock;  // Lock protecting head pointer
    pthread_mutex_t tail_lock;  // Lock protecting tail pointer
} mt_queue_t;

//Queue Initialization
void mt_queue_init(mt_queue_t *q) {
    // Allocate a dummy node; both head and tail start here
    mt_node_t *tmp = malloc(sizeof(mt_node_t));
    tmp->next = NULL;

    q->head = q->tail = tmp;               // Head and tail initially same
    pthread_mutex_init(&q->head_lock, NULL); // Initialize head lock
    pthread_mutex_init(&q->tail_lock, NULL); // Initialize tail lock
}


void mt_enqueue(mt_queue_t *q, int value) {
    // Create a new node to hold the enqueued value
    mt_node_t *tmp = malloc(sizeof(mt_node_t));
    assert(tmp != NULL);
    tmp->value = value;
    tmp->next = NULL;                      // Always the last node

    // Lock the tail so only one thread modifies it at a time
    pthread_mutex_lock(&q->tail_lock);
    q->tail->next = tmp;                   // Link current tail to new node
    q->tail = tmp;                         // Move tail pointer forward
    pthread_mutex_unlock(&q->tail_lock);   // Unlock tail for other threads
}


int mt_dequeue(mt_queue_t *q, int *value) {
    // Lock the head pointer to safely remove a node
    pthread_mutex_lock(&q->head_lock);
    mt_node_t *tmp = q->head;              // Pointer to dummy head
    mt_node_t *new_head = tmp->next;       // Next node in queue

    // If the queue is empty (no real nodes after dummy)
    if (new_head == NULL) {
        pthread_mutex_unlock(&q->head_lock);
        return -1;                         // Indicate queue is empty
    }

    // Read value from the next node
    *value = new_head->value;
    q->head = new_head;                    // Move head pointer forward
    pthread_mutex_unlock(&q->head_lock);   // Unlock head
    free(tmp);                             // Free old dummy node
    return 0;                              // Successful dequeue
}

/*
   LOCK-FREE QUEUE (Michael & Scott)
   */

typedef int lf_data_t;  // Type of data stored in the queue

// Define a node for the lock-free queue
typedef struct lf_node {
    lf_data_t value;                // Node's value
    _Atomic(struct lf_node*) next;  // Atomic pointer to next node
} lf_node_t;

// Define the lock-free queue structure
typedef struct lf_queue {
    _Atomic(lf_node_t*) Head;       // Atomic head pointer
    _Atomic(lf_node_t*) Tail;       // Atomic tail pointer
} lf_queue_t;

//Queue Initialization
void lf_queue_init(lf_queue_t *q) {
    // Create a dummy node for the empty queue
    lf_node_t *node = malloc(sizeof(lf_node_t));
    node->next = NULL;

    // Set both head and tail atomically to point to dummy node
    atomic_store(&q->Head, node);
    atomic_store(&q->Tail, node);
}

//Enqueue Operation 
void lf_enqueue(lf_queue_t *q, lf_data_t value) {
    // Create a new node with the value to enqueue
    lf_node_t *node = malloc(sizeof(lf_node_t));
    node->value = value;
    atomic_store(&node->next, NULL);  // Initially has no next node

    while (1) {
        // Read tail pointer and its next pointer
        lf_node_t *tail = atomic_load(&q->Tail);
        lf_node_t *next = atomic_load(&tail->next);

        // Confirm tail hasn't changed since we read it
        if (atomic_load(&q->Tail) == tail) {
            if (next == NULL) {
                // Tail is pointing to last node, so link the new one
                if (atomic_compare_exchange_strong(&tail->next, &next, node))
                    break;            // Linked successfully
            } else {
                // Tail not pointing to last node → advance tail pointer
                atomic_compare_exchange_strong(&q->Tail, &tail, next);
            }
        }
    }

    // Finally, move tail pointer to the new node (best effort)
    lf_node_t *old_tail = atomic_load(&q->Tail);
    atomic_compare_exchange_strong(&q->Tail, &old_tail, node);
}

//Dequeue Operation
bool lf_dequeue(lf_queue_t *q, lf_data_t *pvalue) {
    while (1) {
        // Read head, tail, and head->next pointers
        lf_node_t *head = atomic_load(&q->Head);
        lf_node_t *tail = atomic_load(&q->Tail);
        lf_node_t *next = atomic_load(&head->next);

        // Check if head is still consistent
        if (atomic_load(&q->Head) == head) {
            if (head == tail) {
                // Queue might be empty or tail is lagging behind
                if (next == NULL)
                    return false;     // Queue empty
                // Advance tail to catch up
                atomic_compare_exchange_strong(&q->Tail, &tail, next);
            } else {
                // Non-empty queue: read next node’s value
                *pvalue = next->value;
                // Try to move head to next node
                if (atomic_compare_exchange_strong(&q->Head, &head, next)) {
                    free(head);       // Free old dummy node
                    return true;      // Successfully dequeued
                }
            }
        }
    }
}

//BENCHMARKING CODE//////////////////////////////////////////////

// Define number of threads and operations per thread
#define NUM_THREADS 4
#define NUM_OPS 100000

// Struct for thread arguments (mutex queue)
typedef struct {
    int id;           // Thread ID
    int nops;         // Number of operations per thread
    mt_queue_t *q;    // Shared queue
} mt_arg_t;

// Struct for thread arguments (lock-free queue)
typedef struct {
    int id;
    int nops;
    lf_queue_t *q;
} lf_arg_t;

//Worker Threads for mutex queue

// Enqueue worker for mutex-based queue
void *mt_enq(void *a_) {
    mt_arg_t *a = a_;
    for (int i = 0; i < a->nops; i++)
        mt_enqueue(a->q, i + a->id * a->nops);  // Enqueue distinct values
    return NULL;
}

// Dequeue worker for mutex-based queue
void *mt_deq(void *a_) {
    mt_arg_t *a = a_;
    int v;
    for (int i = 0; i < a->nops; i++)
        mt_dequeue(a->q, &v);
    return NULL;
}

//Worker Threads for lock-free queue

// Enqueue worker for lock-free queue
void *lf_enq(void *a_) {
    lf_arg_t *a = a_;
    for (int i = 0; i < a->nops; i++)
        lf_enqueue(a->q, i + a->id * a->nops);
    return NULL;
}

// Dequeue worker for lock-free queue
void *lf_deq(void *a_) {
    lf_arg_t *a = a_;
    int v;
    for (int i = 0; i < a->nops; i++)
        lf_dequeue(a->q, &v);
    return NULL;
}

//Some helper functions///

// Measure total time to enqueue and dequeue using mutex queue
double bench_mt(void) {
    mt_queue_t q;
    mt_queue_init(&q);                          // Initialize queue
    pthread_t th[NUM_THREADS];
    mt_arg_t args[NUM_THREADS];
    struct timespec s, e;                       // Start and end time

    clock_gettime(CLOCK_MONOTONIC, &s);         // Start timer

    // Launch threads to enqueue
    for (int i = 0; i < NUM_THREADS; i++) {
        args[i] = (mt_arg_t){ i, NUM_OPS, &q };
        pthread_create(&th[i], NULL, mt_enq, &args[i]);
    }
    for (int i = 0; i < NUM_THREADS; i++)
        pthread_join(th[i], NULL);              // Wait for enqueues to finish

    // Launch threads to dequeue
    for (int i = 0; i < NUM_THREADS; i++)
        pthread_create(&th[i], NULL, mt_deq, &args[i]);
    for (int i = 0; i < NUM_THREADS; i++)
        pthread_join(th[i], NULL);              // Wait for dequeues

    clock_gettime(CLOCK_MONOTONIC, &e);         // End timer

    // Return elapsed time in seconds
    return (e.tv_sec - s.tv_sec) + (e.tv_nsec - s.tv_nsec) / 1e9;
}

// Measure total time to enqueue and dequeue using lock-free queue
double bench_lf(void) {
    lf_queue_t q;
    lf_queue_init(&q);                          // Initialize queue
    pthread_t th[NUM_THREADS];
    lf_arg_t args[NUM_THREADS];
    struct timespec s, e;

    clock_gettime(CLOCK_MONOTONIC, &s);

    // Launch threads to enqueue concurrently
    for (int i = 0; i < NUM_THREADS; i++) {
        args[i] = (lf_arg_t){ i, NUM_OPS, &q };
        pthread_create(&th[i], NULL, lf_enq, &args[i]);
    }
    for (int i = 0; i < NUM_THREADS; i++)
        pthread_join(th[i], NULL);

    // Launch threads to dequeue concurrently
    for (int i = 0; i < NUM_THREADS; i++)
        pthread_create(&th[i], NULL, lf_deq, &args[i]);
    for (int i = 0; i < NUM_THREADS; i++)
        pthread_join(th[i], NULL);

    clock_gettime(CLOCK_MONOTONIC, &e);
    return (e.tv_sec - s.tv_sec) + (e.tv_nsec - s.tv_nsec) / 1e9;
}

//Main Function//////////////////////////////
int main(void) {
    printf("Benchmarking queues with %d threads, %d operations each...\n\n",
           NUM_THREADS, NUM_OPS);

    // Run both benchmarks
    double t1 = bench_mt();
    double t2 = bench_lf();

    // Total number of operations performed (enqueue + dequeue)
    double total = 2.0 * NUM_THREADS * NUM_OPS;

    // Print performance summary
    printf("Mutex-based queue total time: %.6f s\n", t1);
    printf("Lock-free queue total time:   %.6f s\n", t2);
    printf("\nOperations per second for each:\n");
    printf("  Mutex-based: %.2f\n", total / t1);
    printf("  Lock-free:   %.2f\n", total / t2);

    return 0;
}
