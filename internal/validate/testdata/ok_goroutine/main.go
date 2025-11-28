package main

func worker(ch chan int32) {
	ch <- 1
}

func main() {
	ch := make(chan int32, 4)
	go worker(ch)
	<-ch
}
