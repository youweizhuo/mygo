package main

func loop(n int) int {
	if n == 0 {
		return 0
	}
	return loop(n - 1)
}

func main() {
	_ = loop(4)
}
