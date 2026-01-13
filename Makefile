start_docker:
	cd docker && docker-compose up -d

quick_start:
	go run cmd/simulator/main.go --quick

start:
	go run cmd/simulator/main.go
