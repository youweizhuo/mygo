package main

func sink(v int32) {}

func main() {
	var x, y, delta int32
	x = 10
	y = 3
	if x > y {
		delta = x - y
	} else {
		delta = y - x
	}
	sink(delta)
}
