
kconfig: asset.go main.go debug.go
	go build

asset.go: httproot/index.html
	echo "package main" > asset.go
	echo -n "const index string =\"" >>asset.go
	cat httproot/index.html | base64 -w0>>asset.go
	echo "\"" >>asset.go

clean:
	rm -f kconfig asset.go
