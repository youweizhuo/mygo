package main

import "fmt"

const totalCount = 5

func source(out chan<- uint32) {
	out <- totalCount
	for i := uint32(0); i < totalCount; i++ {
		val := i
		if i == 1 {
			val = 0x19700328
		} else if i == 2 {
			val = 0x19700101
		}
		out <- val
		fmt.Printf("input: count %d sent integer 0x%x\n", i, val)
	}
}

func filter(in <-chan uint32, out chan<- uint32) {
	_ = <-in
	out <- totalCount
	for count := uint32(0); count < totalCount; count++ {
		val := <-in
		if val == 0x19700328 {
			val = 0x20050823
		} else if val == 0x19700101 {
			val = 0x20071224
		}
		out <- val
	}
}

func sink(in <-chan uint32, done chan<- bool) {
	_ = <-in
	for count := uint32(0); count < totalCount; count++ {
		val := <-in
		fmt.Printf("output: count %d got integer 0x%x\n", count, val)
	}
	done <- true
}

func main() {
	pipe1 := make(chan uint32, 1)
	pipe2 := make(chan uint32, 4)
	done := make(chan bool, 1)

	go sink(pipe2, done)
	go filter(pipe1, pipe2)
	go source(pipe1)

	finished := <-done
	fmt.Printf("finished is %t\n", finished)
}
