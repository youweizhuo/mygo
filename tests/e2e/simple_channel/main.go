package main

func worker(in <-chan int32, out chan<- int32) {
	v := <-in
	out <- v + 1
}

func main() {
	in := make(chan int32, 4)
	out := make(chan int32, 4)

	go worker(in, out)

	in <- 5
	<-out
}
