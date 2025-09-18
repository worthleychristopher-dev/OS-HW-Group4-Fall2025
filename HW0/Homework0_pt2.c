//Written by Daniel Burns

#include <stdio.h>
#include <stdlib.h>

#define stack_size 100

// Stack structure
typedef struct {
    int data[stack_size];
    int top;
} Stack;

// Initialize stack
void init(Stack *s) {
    s->top = -1; //sets inital location to an under flow scenario
}

// Push to stack
void push(Stack *s, int value) {
    int location = ++(s->top); //location of top spot
    s->data[location] = value; //sets the value to the location of the top spot
    printf("Pushed %d\n", value); //prints the value as its pushed
}

// Pop from stack
int pop(Stack *s) {
    if (s->top < 0) { //checks to see if the stack is empty
        printf("Error: Stack Empty\n");
        return -1;  // Use -1 to indicate error
    }
    int location = (s->top)--; //changes the location to the new spot in the array after being popped
    int value = s->data[location]; //value is taken from the array
    printf("Popped %d\n", value); //prints popped value
    return value;
}

// Main function to test stack
int main() {
    Stack stack;
    init(&stack);

    // Test push
    push(&stack, 10);
    push(&stack, 20);
    push(&stack, 30);

    // Test pop
    pop(&stack);
    pop(&stack);

    // More pushes
    push(&stack, 40);
    push(&stack, 50);

    // Pop all to empty
    pop(&stack);
    pop(&stack);
    pop(&stack);

    // Tests the empty case
    pop(&stack);

    return 0;
}
