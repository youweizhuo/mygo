package main

func worker(_ int) {}

func main() {
	for i := 0; i < 4; i++ {
		go worker(i)
	}
}
