package main

type launcher func()

func worker() {}

func spawn(fn launcher) {
	go fn()
}

func main() {
	spawn(worker)
}
