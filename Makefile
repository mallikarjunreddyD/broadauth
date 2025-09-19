all: bin/cm bin/cmtest bin/owner bin/rcd

.PHONY: clean fmt test

bin:
	mkdir bin

bin/%: cmd/% bin
	go build -o $@ ./$<

clean:
	$(RM) -r -- ./bin

fmt:
	@go fmt ./cmd/* ./internal/* ./pkg/*

test: internal/broadcast internal/slot pkg/hashchain internal/message internal/rx
	cd internal/broadcast && go test -v
	cd internal/message && go test -v
	cd internal/rx && go test -v
	cd internal/slot && go test -v
	cd pkg/hashchain && go test -v
