#define _POSIX_C_SOURCE 199309L //for the time.h constants used to measure in ns
#include <stdio.h>
#include <stdlib.h>
#include <pthread.h>
#include <time.h>
#include <unistd.h>     
#include <sys/wait.h>  

#define count 5 //number of producer and consumer iterations


pthread_mutex_t m = PTHREAD_MUTEX_INITIALIZER; //intializes mutex thread
pthread_cond_t prod = PTHREAD_COND_INITIALIZER; //intializes the producer thread
pthread_cond_t cons = PTHREAD_COND_INITIALIZER; //intializes the consumer thread

int flag=0; //starts the flag at 0 so that the code goes straight to the comsumer thread after outputing the producer

void* producer(void* arg){
    for(int i=1; i<=count; i++){ //goes through the 5 producers
        pthread_mutex_lock(&m); //locks the mutex thread allowing only one thread to use the data at a time
        
        while(flag==1){ //spin lock to keep producer waiting until the thread is free to use
            pthread_cond_wait(&prod, &m); //makes the producer thread wait till the thread is signaled again
        }
        
        printf("Producer: %d\n", i); //prints the producer number
        flag=1; //updates the flag variable to allow the other thread to activate
        
        pthread_cond_signal(&cons); //signals the consumer thread to stop waiting
        pthread_mutex_unlock(&m); //unlocks the mutex thread
    }
}

void* consumer(void* arg){
    for(int i =1; i<=count; i++){ //loops 5 times for the comsumer thread
        pthread_mutex_lock(&m); //locks the mutex thread
        
        while(flag==0){ //spin lock for the consumer thread
            pthread_cond_wait(&cons, &m); //makes the consumer thread waits till signaled
        }
        printf("Consumer: %d\n", i); //prints consumer output
        flag=0; //updates the flag to allow the producer thread to activate
        
        pthread_cond_signal(&prod); //signals the producer thread to stop waiting
        pthread_mutex_unlock(&m); //unlocks the mutex thread
    }
}


int main() {

    struct timespec start_time1, end_time1; //structs to hold the time in ns to count the time each approach takes
    struct timespec start_time2, end_time2;
    long nanoseconds;

    // Get the starting time for IPC code
    clock_gettime(CLOCK_MONOTONIC, &start_time1); 

    //HW0 IPC Code to compare//////////////////////////////////////////////////////////////
    int parent_to_child[2]; //pipe var for communicating from parent to child
    int child_to_parent[2]; //pipe var for communicating from child to parent
    pipe(parent_to_child); //create parent to child com pipe
    pipe(child_to_parent); //create child to parent com pipe
    pid_t pid=fork(); //create the 2 instances, parent and child
    int x; //variable used to track the done message from child to parent

    if (pid==0){ //true if is a child process and a consumer
        close(parent_to_child[1]); //close write end of parent to child
        close(child_to_parent[0]); //close read end of child to parent
        for (int i=1;i<=5;i++){ //stop after 5 numbers
            read(parent_to_child[0],&x,sizeof(x)); //read the message from parent
            printf("Consumer: %d\n",x); //print message
            write(child_to_parent[1],"1",1); //send message saying child is done with its read and print
        }
        
    // Get the ending time
    clock_gettime(CLOCK_MONOTONIC, &end_time1);
    // Calculate the difference in nanoseconds
    nanoseconds = (end_time1.tv_sec - start_time1.tv_sec) * 1000000000L + 
                  (end_time1.tv_nsec - start_time1.tv_nsec);

    printf("Elapsed time for IPC approach: %ld nanoseconds\n", nanoseconds);

    }
    else{ //parent process if not 0, so producer
        close(child_to_parent[1]); //close write end of child to parent
        close(parent_to_child[0]); //close read end of parent to child
        for (int i=1;i<=5;i++){ //stop after 5 numbers
            printf("Producer: %d\n",i); //print message 
            write(parent_to_child[1],&i,sizeof(i)); //write number, i in this case, into the pipe
            read(child_to_parent[0],&x,sizeof(x)); //wait for message from child
        }

        //HW1 New code for threads to compare to IPC, in parent process so isn't ran twice/////////////////////////////////////////////////////
        pthread_t prod; //intializes the threads consumer and producer
        pthread_t cons;

        clock_gettime(CLOCK_MONOTONIC, &start_time2);
        
        pthread_create(&prod, NULL, producer, NULL); //creates each of the threads
        pthread_create(&cons, NULL, consumer, NULL);
        
        pthread_join(prod, NULL); //waits for the threads to finish
        pthread_join(cons, NULL);

        // Get the ending time
        clock_gettime(CLOCK_MONOTONIC, &end_time2);

        // Calculate the difference in nanoseconds
        nanoseconds = (end_time2.tv_sec - start_time2.tv_sec) * 1000000000L + 
                    (end_time2.tv_nsec - start_time2.tv_nsec);

        printf("Elapsed time for threads approach: %ld nanoseconds\n", nanoseconds);
        //////////////////////////////////////////////////////////////////////////////////////////////////

    }

    

    return 0;
}