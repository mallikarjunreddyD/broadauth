test: internal/broadcast internal/slot
	cd internal/broadcast && go test -v
	cd internal/slot && go test -v
