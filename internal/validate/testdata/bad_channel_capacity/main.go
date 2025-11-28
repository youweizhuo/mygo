package main

func depth() int {
	return 4
}

func main() {
	_ = make(chan int, depth())
	_ = make(chan int8)
}
