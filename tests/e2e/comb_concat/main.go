package main

import "fmt"

// Exercises concat/extract by assembling/disassembling bytes similar to CIRCT comb tests.
func main() {
	var upper uint8 = 0xAB
	var lower uint8 = 0xCD
	word := uint16(upper)<<8 | uint16(lower)
	highNibble := (word >> 12) & 0xF
	lowByte := word & 0xFF

	fmt.Printf("word=0x%x high=%d low=0x%x\n", word, highNibble, lowByte)
}
