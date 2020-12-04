init:
	mkdir -p ./.dgraph/zero
	mkdir -p ./.dgraph/alpha

fmt:
	go fmt ./...

test:
	go test ./...