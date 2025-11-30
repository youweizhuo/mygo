package main

import "fmt"

// Inspired by CIRCT comb adder tests, ensures signed/unsigned additions share
// the same lowering path.
func main() {
	var a int16 = -12
	var b int16 = 25
	var carry uint16 = 0x1234

	partial := int32(a) + int32(b)
	widened := uint32(uint16(partial)) + uint32(carry)
	fmt.Printf("partial=%d widened=0x%x\n", partial, widened)
}
