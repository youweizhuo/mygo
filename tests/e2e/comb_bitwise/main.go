package main

import "fmt"

// Mirrors CIRCT comb bitwise coverage by mixing and/or/xor masks.
func main() {
	var mask uint16 = 0x33CC
	var data uint16 = 0x0F0F

	andVal := mask & data
	orVal := mask | data
	xorVal := mask ^ data
	fmt.Printf("and=0x%x or=0x%x xor=0x%x\n", andVal, orVal, xorVal)
}
