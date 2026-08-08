test:
    go test -count=1 ./...

fmt:
    gofumpt -w ./

fix:
    jj fix
