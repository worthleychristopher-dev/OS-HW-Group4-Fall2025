//Written by Chris Worthley
//To run, simply click "Run" when in a linux environment, I used Ubuntu to create it

#include <unistd.h>     
#include <sys/wait.h>   
#include <stdio.h>      
#include <stdlib.h>     

int main(){

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
        


    }
    else{ //parent process if not 0, so producer
        close(child_to_parent[1]); //close write end of child to parent
        close(parent_to_child[0]); //close read end of parent to child
        for (int i=1;i<=5;i++){ //stop after 5 numbers
            printf("Producer: %d\n",i); //print message 
            write(parent_to_child[1],&i,sizeof(i)); //write number, i in this case, into the pipe
            read(child_to_parent[0],&x,sizeof(x)); //wait for message from child
        }

    }


    return 0;
}