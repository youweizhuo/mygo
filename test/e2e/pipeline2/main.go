package main

import "fmt"

const totalPairs = 4

func stage1(out chan<- uint32) {
	out <- totalPairs
	for i := uint32(0); i < totalPairs; i++ {
		value := i + i
		out <- value
		fmt.Printf("stage 1: sent integer %d\n", value)
	}
}

func stage2(in <-chan uint32, out chan<- byte) {
	_ = <-in
	sendUint32(totalPairs, out)
	for count := uint32(0); count < totalPairs; count++ {
		val := <-in
		sendUint32(val, out)
		fmt.Printf("stage 2: emitted 4 bytes for %d\n", val)
	}
}

func stage3(in <-chan byte, done chan<- bool) {
	_ = readUint32(in)
	for count := uint32(0); count < totalPairs; count++ {
		value := readUint32(in)
		fmt.Printf("stage 3: reconstructed integer %d\n", value)
	}
	done <- true
}

func sendUint32(value uint32, out chan<- byte) {
	out <- byte((value >> 24) & 0xFF)
	out <- byte((value >> 16) & 0xFF)
	out <- byte((value >> 8) & 0xFF)
	out <- byte(value & 0xFF)
}

func readUint32(in <-chan byte) uint32 {
	b0 := uint32(<-in)
	b1 := uint32(<-in)
	b2 := uint32(<-in)
	b3 := uint32(<-in)
	return (b0 << 24) | (b1 << 16) | (b2 << 8) | b3
}

func main() {
	pipe1 := make(chan uint32, 1)
	pipe2 := make(chan byte, 8)
	done := make(chan bool, 1)

	go stage3(pipe2, done)
	go stage2(pipe1, pipe2)
	go stage1(pipe1)

	finished := <-done
	fmt.Printf("finished is %t\n", finished)
}
