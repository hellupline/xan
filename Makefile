xan: main.go go.mod go.sum
	go build

run: xan
	./$<
