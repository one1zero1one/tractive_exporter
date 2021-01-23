build:
	echo "Compiling for local"
	go build -o bin/tractive_exporter main.go

compile:
	echo "Compiling for every OS and Platform"
	#GOOS=linux GOARCH=arm go build -o bin/tractive_exporter_linux main.go

run:
	echo "Running local"
	go run main.go
