package main

import "fmt"

const (
	numPackets = 4
	numPorts   = 2
	fieldSrc   = 0
	fieldDest  = 1
	fieldData  = 2
)

type packet [3]int32

func producer(id int32, baseDest int32, out chan<- packet, ack <-chan bool) {
	for i := int32(0); i < numPackets; i++ {
		dest := (baseDest + i) & 1
		out <- packet{id, dest, id*10 + i}
		<-ack
	}
}

func router(left, right <-chan packet, ackLeft, ackRight chan<- bool, outA, outB chan<- packet, readyA, readyB <-chan bool) {
	for i := int32(0); i < numPackets; i++ {
		routePacket(<-left, outA, outB, readyA, readyB)
		ackLeft <- true
		routePacket(<-right, outA, outB, readyA, readyB)
		ackRight <- true
	}
}

func routePacket(pkt packet, outA, outB chan<- packet, readyA, readyB <-chan bool) {
	if pkt[fieldDest]&1 == 0 {
		outA <- pkt
		<-readyA
	} else {
		outB <- pkt
		<-readyB
	}
}

func consumer(id int32, in <-chan packet, ready chan<- bool, done chan<- bool) {
	for i := int32(0); i < numPackets; i++ {
		pkt := <-in
		fmt.Printf("consumer %d got packet from %d with payload %d\n", id, pkt[fieldSrc], pkt[fieldData])
		ready <- true
	}
	done <- true
}

func main() {
	left := make(chan packet, 1)
	right := make(chan packet, 1)
	outA := make(chan packet, 1)
	outB := make(chan packet, 1)
	ackLeft := make(chan bool, 1)
	ackRight := make(chan bool, 1)
	readyA := make(chan bool, 1)
	readyB := make(chan bool, 1)
	done := make(chan bool, numPorts)

	go consumer(0, outA, readyA, done)
	go consumer(1, outB, readyB, done)
	go router(left, right, ackLeft, ackRight, outA, outB, readyA, readyB)
	go producer(0, 0, left, ackLeft)
	go producer(1, 1, right, ackRight)

	for i := 0; i < numPorts; i++ {
		<-done
	}
	fmt.Println("router complete")
}
