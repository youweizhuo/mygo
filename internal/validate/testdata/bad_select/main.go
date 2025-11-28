package main

func main() {
	ch := make(chan int, 1)
	select {
	case <-ch:
	default:
	}
}
